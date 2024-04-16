/*
Copyright 2024 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package exec

import (
	"context"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/google/go-cmp/cmp"
)

func TestParallelExecutor(t *testing.T) {
	for _, tc := range []struct {
		name  string
		graph string
		// pending should be sorted alphabetically for comparison.
		pending []string
		wantErr bool
	}{
		{
			name:  "empty graph",
			graph: "",
		},
		{
			name:  "one action",
			graph: "A",
		},
		{
			name:  "action and dependency",
			graph: "A -> B",
		},
		{
			name:  "chain of 3 actions",
			graph: "A -> B -> C",
		},
		{
			name:  "two chains with common root",
			graph: "A -> B -> C; A -> C",
		},
		{
			name:    "two node cycle",
			graph:   "A -> B -> A",
			pending: []string{"A", "B"},
			wantErr: true,
		},
		{
			name:  "lot of children",
			graph: "A -> B; A -> C; A -> D -> B; A -> E -> F; A -> G",
		},
		{
			name:  "complex fan in",
			graph: "A -> Z; B -> Z; C -> D -> B",
		},
		{
			name:    "cycle in larger graph",
			graph:   "A -> B -> C -> D -> C; X -> Y",
			pending: []string{"C", "D"},
			wantErr: true,
		},
		{
			name:    "error in action",
			graph:   "A -> B -> !C -> D",
			pending: []string{"D"},
			wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mockCloud := cloud.NewMockGCE(&cloud.SingleProjectRouter{ID: "proj1"})
			actions := actionsFromGraphStr(tc.graph)

			ex, err := NewParallelExecutor(mockCloud,
				actions,
				TimeoutOption(1*time.Minute),
				ErrorStrategyOption(StopOnError))
			if err != nil {
				t.Fatalf("NewParallelExecutor(_, _) = %v, want nil", err)
			}
			result, err := ex.Run(context.Background())
			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Fatalf("ex.Run(_, _) = %v; gotErr = %t, want %t", err, gotErr, tc.wantErr)
			}
			got := sortedStrings(result.Pending, func(a Action) string { return a.(*testAction).name })
			if diff := cmp.Diff(got, tc.pending); diff != "" {
				t.Errorf("pending: diff -got,+want: %s", diff)
			}
		})
	}
}

func TestParallelExecutorErrorStrategy(t *testing.T) {
	for _, tc := range []struct {
		name  string
		graph string
		// pending should be sorted alphabetically for comparison.
		pending []string
		errs    []string
	}{
		{
			name:    "linear graph",
			graph:   "A -> !B -> C -> D -> E",
			pending: []string{"C", "D", "E"},
			errs:    []string{"B"},
		},
		{
			name:    "branched graph",
			graph:   "A -> !B -> C; A -> D; A -> E; A -> F",
			pending: []string{"C"},
			errs:    []string{"B"},
		},
	} {
		mockCloud := cloud.NewMockGCE(&cloud.SingleProjectRouter{ID: "proj1"})
		actions := actionsFromGraphStr(tc.graph)

		for _, strategy := range []ErrorStrategy{StopOnError, ContinueOnError} {
			name := tc.name + " " + string(strategy)
			t.Run(name, func(t *testing.T) {
				ex, err := NewParallelExecutor(mockCloud,
					actions,
					ErrorStrategyOption(strategy),
					TimeoutOption(10*time.Second))
				if err != nil {
					t.Fatalf("NewParallelExecutor() = %v, want nil", err)
				}
				result, err := ex.Run(context.Background())
				if err == nil {
					t.Fatalf("Run() = %v; expected error", err)
				}
				gotErrs := sortedStrings(result.Errors, func(a ActionWithErr) string { return a.Action.(*testAction).name })

				if diff := cmp.Diff(gotErrs, tc.errs); diff != "" {
					t.Errorf("errors: diff -got,+want: %s", diff)
				}
				got := sortedStrings(result.Pending, func(a Action) string { return a.(*testAction).name })
				if diff := cmp.Diff(got, tc.pending); diff != "" {
					t.Errorf("pending: diff -got,+want: %s", diff)
				}
			})
		}
	}
}
