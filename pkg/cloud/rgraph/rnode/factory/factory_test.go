// /*
// Copyright 2023 Google LLC

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

// https://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
// */

package rnode

import (
	"context"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/rgraph"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/rgraph/rnode"
	alpha "google.golang.org/api/compute/v0.alpha"
	"google.golang.org/api/compute/v1"
)

func defaultHTTPHC() *compute.HealthCheck {
	return &compute.HealthCheck{
		Name:             "rgraph-hc",
		CheckIntervalSec: 10,
		HttpHealthCheck: &compute.HTTPHealthCheck{
			Port:     80,
			PortName: "http",
		},
		Type: "HTTP",
	}
}

func alphaHTTPHC() *alpha.HealthCheck {
	return &alpha.HealthCheck{
		Name:             "rgraph-hc",
		CheckIntervalSec: 10,
		HttpHealthCheck: &alpha.HTTPHealthCheck{
			Port:     80,
			PortName: "http",
		},
		Type: "HTTP",
	}
}

func TestGenericFactoryErrorCase(t *testing.T) {
	pr := &cloud.SingleProjectRouter{ID: "proj1"}
	f := NewFactory(pr, cloud.SimpleVersionResolver{})
	hcf := f.HealthCheck(context.Background(), meta.RegionalScope)
	hc := alphaHTTPHC()

	// default HC version is set to GA expect error on creating builder.
	_, err := hcf.CreateBuilderGeneric(hc, rnode.NodeExists, "us-central1")
	if err == nil {
		t.Fatalf("hcf.CreateBuilder = nil, want errror")
	}
}
func TestGenericFactory(t *testing.T) {
	pr := &cloud.SingleProjectRouter{ID: "proj1"}
	cv := cloud.CustomVersion{
		Key: cloud.VersionResolverKey{
			Service: "healthChecks",
			Scope:   meta.RegionalScope,
		},
		Version: meta.VersionGA,
	}
	vr := cloud.NewCustomResolver(cv)
	f := NewFactory(pr, vr)
	hcf := f.HealthCheck(context.Background(), meta.RegionalScope)
	hc := defaultHTTPHC()
	hcb, err := hcf.CreateBuilderGeneric(hc, rnode.NodeExists, "us-central1")
	if err != nil {
		t.Fatalf("hcf.CreateBuilder = _, %v, want nil", err)
	}

	gr := rgraph.NewBuilder()
	gr.Add(*hcb)
	g, err := gr.Build()
	if err != nil {
		t.Errorf("gr.Build() = _, %v, want nil ", err)
	}

	for i, a := range g.All() {
		t.Logf("Node[%v] %v", i, a.ID().String())
	}
}

func TestGAFactory(t *testing.T) {
	pr := &cloud.SingleProjectRouter{ID: "proj1"}
	hc := defaultHTTPHC()

	f := NewFactory(pr, cloud.SimpleVersionResolver{})
	hcf := f.HealthCheck(context.Background(), meta.RegionalScope)
	hcb := hcf.CreateBuilderGA(*hc, rnode.NodeExists, "us-central1")
	hcb2 := hcf.CreateBuilderGA(*hc, rnode.NodeExists, "us-central2")

	gr := rgraph.NewBuilder()
	gr.Add(*hcb)
	gr.Add(*hcb2)
	g, err := gr.Build()
	if err != nil {
		t.Errorf("gr.Build() = _, %v, want nil ", err)
	}

	for i, a := range g.All() {
		t.Logf("Node[%v] %v", i, a.ID().String())
	}
}
