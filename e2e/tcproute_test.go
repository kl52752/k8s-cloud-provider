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
package e2e

import (
	"context"
	"fmt"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/rgraph"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/rgraph/exec"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/rgraph/rnode"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/rgraph/rnode/backendservice"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/rgraph/rnode/healthcheck"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/rgraph/rnode/networkendpointgroup"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/rgraph/rnode/tcproute"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/rgraph/workflow/plan"
	"github.com/kr/pretty"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/networkservices/v1"
)

const (
	meshURL = "https://networkservices.googleapis.com/v1alpha1/projects/katarzynalach-gke-dev/locations/global/meshes/mcs-mesh"
	negName = "k8s1-3894226e-default-foo-backend-80-28ec9cf3"
	zone    = "us-central1-c"
)

func TestTcpRoute(t *testing.T) {
	t.Parallel()
	t.Logf("starting test")
	ctx := context.Background()

	bs := &compute.BackendService{
		Name:                resourceName("bs1-tcp"),
		Backends:            []*compute.Backend{},
		LoadBalancingScheme: "INTERNAL_SELF_MANAGED",
		Protocol:            "TCP",
	}
	bsKey := meta.GlobalKey(bs.Name)

	// t.Cleanup(func() {
	// 	err := theCloud.BackendServices().Delete(ctx, bsKey)
	// 	t.Logf("bs delete: %v", err)
	// })
	t.Logf("insert BS")
	// TcpRoute needs a BackendService to point to.
	err := theCloud.BackendServices().Insert(ctx, bsKey, bs)
	t.Logf("bs insert: %v", err)
	if err != nil {
		t.Errorf("theCloud.BackendServices().Insert %v", err)
	}
	t.Logf("backend service inserted %s", bs.Name)

	// Current API does not support the new URL scheme.
	serviceName := fmt.Sprintf("https://compute.googleapis.com/v1/projects/%s/global/backendServices/%s", testFlags.project, bs.Name)
	tcpr := &networkservices.TcpRoute{
		Name:   resourceName("route1"),
		Meshes: []string{meshURL},
		Rules: []*networkservices.TcpRouteRouteRule{
			{
				Action: &networkservices.TcpRouteRouteAction{
					Destinations: []*networkservices.TcpRouteRouteDestination{
						{ServiceName: serviceName},
					},
				},
			},
		},
	}
	t.Logf("tcpr = %s", pretty.Sprint(tcpr))
	tcprKey := meta.GlobalKey(tcpr.Name)

	// Insert
	// t.Cleanup(func() {
	// 	err := theCloud.TcpRoutes().Delete(ctx, tcprKey)
	// 	t.Logf("tcpRoute delete: %v", err)
	// })

	err = theCloud.TcpRoutes().Insert(ctx, tcprKey, tcpr)
	t.Logf("tcproutes insert: %v", err)
	if err != nil {
		t.Errorf("Insert() = %v", err)
	}

	// Get
	tcpRoute, err := theCloud.TcpRoutes().Get(ctx, tcprKey)
	t.Logf("tcpRoute = %s", pretty.Sprint(tcpRoute))
	if err != nil {
		t.Errorf("Get(%s) = %v", tcprKey, err)
	}

	if len(tcpRoute.Rules) < 1 || len(tcpRoute.Rules[0].Action.Destinations) < 1 {
		t.Errorf("gotTcpRoute = %s, need at least one destination", pretty.Sprint(tcpRoute))
	}
	gotServiceName := tcpRoute.Rules[0].Action.Destinations[0].ServiceName
	if gotServiceName != serviceName {
		t.Errorf("gotTcpRoute = %s, gotServiceName = %q, want %q", pretty.Sprint(tcpRoute), gotServiceName, serviceName)
	}
}

func resourceSelfLink(id *cloud.ResourceID) string {
	apiGroup := meta.APIGroupCompute
	relName := cloud.RelativeResourceName(id.ProjectID, id.Resource, id.Key)
	prefix := fmt.Sprintf("https://%s.googleapis.com/v1", apiGroup)
	return prefix + "/" + relName
}

func buildNEG(graphBuilder *rgraph.Builder, name, zone string) (*cloud.ResourceID, error) {
	negID := networkendpointgroup.ID(testFlags.project, meta.ZonalKey(name, zone))
	negMut := networkendpointgroup.NewMutableNetworkEndpointGroup(testFlags.project, negID.Key)
	negMut.Access(func(x *compute.NetworkEndpointGroup) {
		x.Zone = zone
	})

	negRes, err := negMut.Freeze()
	if err != nil {
		return nil, err
	}
	negBuilder := networkendpointgroup.NewBuilder(negID)
	negBuilder.SetOwnership(rnode.OwnershipExternal)
	negBuilder.SetState(rnode.NodeExists)
	negBuilder.SetResource(negRes)
	graphBuilder.Add(negBuilder)
	return negID, nil
}

func buildHealthCheck(graphBuilder *rgraph.Builder, name string, checkIntervalSec int64) (*cloud.ResourceID, error) {
	hcID := healthcheck.ID(testFlags.project, meta.GlobalKey(resourceName(name)))
	hcMutRes := healthcheck.NewMutableHealthCheck(testFlags.project, hcID.Key)
	hcMutRes.Access(func(x *compute.HealthCheck) {
		x.CheckIntervalSec = checkIntervalSec
		x.HealthyThreshold = 5
		x.TimeoutSec = 6
		x.Type = "HTTP"
		x.HttpHealthCheck = &compute.HTTPHealthCheck{
			RequestPath: "/",
			Port:        int64(9376),
		}
	})
	hcRes, err := hcMutRes.Freeze()
	if err != nil {
		return nil, err
	}
	hcBuilder := healthcheck.NewBuilder(hcID)
	hcBuilder.SetOwnership(rnode.OwnershipManaged)
	hcBuilder.SetState(rnode.NodeExists)
	hcBuilder.SetResource(hcRes)
	graphBuilder.Add(hcBuilder)
	return hcID, nil
}

func buildBackendService(graphBuilder *rgraph.Builder, name string, hcID, negID *cloud.ResourceID) (*cloud.ResourceID, error) {
	bsID := backendservice.ID(testFlags.project, meta.GlobalKey(resourceName(name)))

	bsMutResource := backendservice.NewMutableBackendService(testFlags.project, bsID.Key)
	bsMutResource.Access(func(x *compute.BackendService) {
		x.LoadBalancingScheme = "INTERNAL_SELF_MANAGED"
		x.Protocol = "TCP"
		x.Port = 80
		x.Backends = []*compute.Backend{
			{
				Group:          negID.SelfLink(meta.VersionGA),
				BalancingMode:  "CONNECTION",
				MaxConnections: 10,
			},
		}
		x.HealthChecks = []string{hcID.SelfLink(meta.VersionGA)}
	})
	bsResource, err := bsMutResource.Freeze()
	if err != nil {
		return nil, err
	}

	bsBuilder := backendservice.NewBuilder(bsID)
	bsBuilder.SetOwnership(rnode.OwnershipManaged)
	bsBuilder.SetState(rnode.NodeExists)
	bsBuilder.SetResource(bsResource)

	graphBuilder.Add(bsBuilder)
	return bsID, nil
}

func buildTCPRoute(graphBuilder *rgraph.Builder, name string, bsID *cloud.ResourceID) (*cloud.ResourceID, error) {
	tcpID := tcproute.ID(testFlags.project, meta.GlobalKey(resourceName(name)))
	tcpMutRes := tcproute.NewMutableTcpRoute(testFlags.project, tcpID.Key)

	tcpMutRes.Access(func(x *networkservices.TcpRoute) {
		x.Description = "tcp route for rGraph test"
		x.Name = tcpID.Key.Name
		x.Meshes = []string{meshURL}
		x.Rules = []*networkservices.TcpRouteRouteRule{
			{
				Action: &networkservices.TcpRouteRouteAction{
					Destinations: []*networkservices.TcpRouteRouteDestination{
						{
							ServiceName: resourceSelfLink(bsID),
							Weight:      10,
						},
					},
				},
				Matches: []*networkservices.TcpRouteRouteMatch{
					{
						Address: "10.240.3.83/32",
						Port:    "80",
					},
				},
			},
		}
	})

	tcpRes, err := tcpMutRes.Freeze()
	if err != nil {
		return nil, err
	}

	tcpRouteBuilder := tcproute.NewBuilder(tcpID)
	tcpRouteBuilder.SetOwnership(rnode.OwnershipManaged)
	tcpRouteBuilder.SetState(rnode.NodeExists)
	tcpRouteBuilder.SetResource(tcpRes)

	graphBuilder.Add(tcpRouteBuilder)
	return tcpID, nil
}

func TestRgraphLB(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	graphBuilder := rgraph.NewBuilder()
	negID, err := buildNEG(graphBuilder, negName, zone)
	if err != nil {
		t.Fatalf("buildNEG(_, %s, %s) = (_, %v), want (_, nil)", negName, zone, err)
	}

	hcID, err := buildHealthCheck(graphBuilder, "hc-test", 15)
	if err != nil {
		t.Fatalf("buildHealthCheck(_, hc-test, _) = (_, %v), want (_, nil)", err)
	}
	bsID, err := buildBackendService(graphBuilder, "bs-test", hcID, negID)
	if err != nil {
		t.Fatalf("buildBackendService(_, bs-test, _, _) = (_, %v), want (_, nil)", err)
	}

	_, err = buildTCPRoute(graphBuilder, "tcproute-test", bsID)
	if err != nil {
		t.Fatalf("buildBackendService(_, tcproute-test, _) = (_, %v), want (_, nil)", err)
	}

	graph, err := graphBuilder.Build()
	if err != nil {
		t.Fatalf("graphBuilder.Build() = %v, want nil", err)
	}

	result, err := plan.Do(ctx, theCloud, graph)
	if err != nil {
		t.Fatalf("plan.Do(_, _, _) = %v, want nil", err)
	}

	var viz exec.GraphvizTracer
	ex, err := exec.NewSerialExecutor(result.Actions, exec.TracerOption(&viz))
	if err != nil {
		return
	}
	res, err := ex.Run(context.Background(), theCloud)
	if err != nil || res == nil {
		t.Errorf("ex.Run(_,_) = ( %v, %v), want (*result, nil)", res, err)
	}

	t.Logf("exec got Result.Completed len(%d) =\n%v", len(res.Completed), res.Completed)
	t.Logf("exec got Result.Errors len(%d) =\n%v", len(res.Errors), res.Errors)
	t.Logf("exec got Result.Pending len(%d) =\n%v", len(res.Pending), res.Pending)
}
