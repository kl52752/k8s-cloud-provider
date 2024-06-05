/*
Copyright 2023 Google LLC

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
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/google/go-cmp/cmp"
)

func sortedStrings[T any](l []T, f func(T) string) []string {
	var ret []string
	for _, x := range l {
		ret = append(ret, f(x))
	}
	sort.Strings(ret)
	return ret
}

func TestSerialExecutor(t *testing.T) {
	for _, dryRun := range []string{"dry run", "normal run"} {
		t.Run(dryRun, func(t *testing.T) {
			for _, tc := range []struct {
				name  string
				graph string
				// pending should be sorted alphabetically for comparison.
				pending []string
				errs    []string
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
				},
				{
					name:  "complex fan in",
					graph: "A -> Z; B -> Z; C -> D -> B",
				},
				{
					name:    "cycle in larger graph",
					graph:   "A -> B -> C -> D -> C; X -> Y",
					pending: []string{"C", "D"},
				},
				{
					name:    "error in action",
					graph:   "A -> B -> !C -> D",
					pending: []string{"D"},
					errs:    []string{"C([C])"},
					wantErr: true,
				},
			} {
				if dryRun == "dry run" && tc.wantErr {
					// Dry run assumes no errors happen, so skip these test cases.
					return
				}
				t.Run(tc.name, func(t *testing.T) {

					t.Logf("Graph: %q", tc.graph)
					actions := actionsFromGraphStr(tc.graph)

					tr := NewGraphvizTracer()
					ex, err := NewSerialExecutor(nil,
						actions,
						ErrorStrategyOption(StopOnError),
						TracerOption(tr),
						DryRunOption(dryRun == "dry run"))
					if err != nil {
						t.Fatalf("NewSerialExecutor() = %v, want nil", err)
					}
					result, err := ex.Run(context.Background())
					if gotErr := err != nil; gotErr != tc.wantErr {
						t.Fatalf("Run() = %v; gotErr = %t, want %t", err, gotErr, tc.wantErr)
					}
					got := sortedStrings(result.Pending, func(a Action) string { return a.(*testAction).name })
					if diff := cmp.Diff(got, tc.pending); diff != "" {
						t.Errorf("pending: diff -got,+want: %s", diff)
					}

					var errNames []string
					for _, ae := range result.Errors {
						errNames = append(errNames, ae.Action.Metadata().Name)
					}
					sort.Strings(errNames)
					if diff := cmp.Diff(errNames, tc.errs); diff != "" {
						t.Errorf("errors: diff -got,+want: %s", diff)
					}

					t.Log(tr.String())
				})
			}
		})
	}
}

func TestSerialExecutorErrorStrategy(t *testing.T) {
	for _, tc := range []struct {
		name     string
		graph    string
		strategy ErrorStrategy
		// pending should be sorted alphabetically for comparison.
		pending []string
		errs    []string
		wantErr bool
	}{
		{
			name:     "stop on error",
			graph:    "A -> !B -> C -> D -> E",
			strategy: StopOnError,
			pending:  []string{"C", "D", "E"},
			errs:     []string{"B"},
			wantErr:  true,
		},
		{
			name:     "continue on error",
			graph:    "A -> !B -> C -> D -> E",
			strategy: ContinueOnError,
			pending:  nil,
			errs:     []string{"B"},
			wantErr:  true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("Graph: %q", tc.graph)
			actions := actionsFromGraphStr(tc.graph)

			var tr GraphvizTracer
			ex, err := NewSerialExecutor(nil,
				actions,
				ErrorStrategyOption(tc.strategy),
				TracerOption(&tr),
				DryRunOption(false))
			if err != nil {
				t.Fatalf("NewSerialExecutor() = %v, want nil", err)
			}
			result, err := ex.Run(context.Background())
			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Fatalf("Run() = %v; gotErr = %t, want %t", err, gotErr, tc.wantErr)
			}
			got := sortedStrings(result.Pending, func(a Action) string { return a.(*testAction).name })
			if diff := cmp.Diff(got, tc.pending); diff != "" {
				t.Errorf("pending: diff -got,+want: %s", diff)
			}
			got = sortedStrings(result.Errors, func(a ActionWithErr) string { return a.Action.(*testAction).name })
			if diff := cmp.Diff(got, tc.errs); diff != "" {
				t.Errorf("errors: diff -got,+want: %s", diff)
			}
			t.Log(tr.String())
		})
	}
}

func TestSerialExecutorTimeoutOptions(t *testing.T) {
	for _, tc := range []struct {
		name string

		execTimeout  time.Duration
		eventTimeout time.Duration
		wantErr      bool

		// actions should be sorted alphabetically for comparison.
		completed []string
		errors    []string
		pending   []string
	}{
		{
			name:         "All actions should finish within timeout",
			execTimeout:  30 * time.Second,
			eventTimeout: 0 * time.Second,
			completed:    []string{"A", "B"},
		},
		{
			name:         "Actions longer than timeout",
			execTimeout:  1 * time.Millisecond,
			eventTimeout: 30 * time.Second,
			completed:    []string{"A"},
			errors:       []string{"B"},
			wantErr:      true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Prepare actions A -> B; where B is long lasting operation.
			a := &testAction{name: "A", events: EventList{StringEvent("A")}}
			b := &testAction{
				name:   "B",
				events: EventList{StringEvent("B")},
				runHook: func(ctx context.Context) error {
					if tc.eventTimeout > 0 {
						ticker := time.NewTicker(tc.eventTimeout)
						select {
						case <-ticker.C:
						case <-ctx.Done():
							return errors.New("context canceled")
						}
					}
					return nil
				},
			}
			b.Want = EventList{StringEvent("A")}

			mockCloud := cloud.NewMockGCE(&cloud.SingleProjectRouter{ID: "proj1"})
			ex, err := NewSerialExecutor(mockCloud,
				[]Action{a, b},
				TimeoutOption(tc.execTimeout),
			)
			if err != nil {
				t.Fatalf("NewSerialExecutor(_, _, %v) = %v; want nil", tc.execTimeout, err)
			}
			result, err := ex.Run(context.Background())

			t.Logf("result.Completed: %v", result.Completed)
			t.Logf("result.Error: %v", result.Errors)
			t.Logf("result.Pending: %v", result.Pending)

			gotErr := err != nil
			if tc.wantErr != gotErr {
				t.Fatalf("ex.Run(_) = %v, got error: %v want error: %v", err, gotErr, tc.wantErr)
			}

			cmpA := func(desc string, got []Action, want []string) {
				gotS := sortedStrings(got, func(a Action) string { return a.(*testAction).name })
				if diff := cmp.Diff(gotS, want); diff != "" {
					t.Errorf("%s: diff -got,+want: %s", desc, diff)
				}
			}
			cmpAE := func(desc string, got []ActionWithErr, want []string) {
				gotS := sortedStrings(got, func(a ActionWithErr) string { return a.Action.(*testAction).name })
				if diff := cmp.Diff(gotS, want); diff != "" {
					t.Errorf("%s: diff -got,+want: %s", desc, diff)
				}
			}
			cmpA("completed", result.Completed, tc.completed)
			cmpAE("errors", result.Errors, tc.errors)
			cmpA("pending", result.Pending, tc.pending)
		})
	}
}
