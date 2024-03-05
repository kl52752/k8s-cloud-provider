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
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/rgraph/rnode/networkendpointgroup"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/rgraph/rnode/tcproute"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/rgraph/workflow/plan"
	"github.com/kr/pretty"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/networkservices/v1"
)

const (
	meshName   = "test-mesh"
	negName    = "k8s1-3894226e-default-foo-backend-80-28ec9cf3"
	negName2   = "k8s1-54982af5-default-bar-svc-8080-5aabd6b1"
	zone       = "us-central1-c"
	routeCIDR  = "10.240.3.83/32"
	routeCIRD2 = "10.240.4.83/32"
)

func TestTcpRoute(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	bs := &compute.BackendService{
		Name:                resourceName("bs1"),
		Backends:            []*compute.Backend{},
		LoadBalancingScheme: "INTERNAL_SELF_MANAGED",
	}
	bsKey := meta.GlobalKey(bs.Name)

	t.Cleanup(func() {
		err := theCloud.BackendServices().Delete(ctx, bsKey)
		t.Logf("bs delete: %v", err)
	})

	// TcpRoute needs a BackendService to point to.
	err := theCloud.BackendServices().Insert(ctx, bsKey, bs)
	t.Logf("bs insert: %v", err)
	if err != nil {
		t.Fatal(err)
	}

	// Current API does not support the new URL scheme.
	serviceName := fmt.Sprintf("https://compute.googleapis.com/v1/projects/%s/global/backendServices/%s", testFlags.project, bs.Name)
	tcpr := &networkservices.TcpRoute{
		Name: resourceName("route1"),
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

	t.Cleanup(func() {
		err := theCloud.TcpRoutes().Delete(ctx, tcprKey)
		t.Logf("tcpRoute delete: %v", err)
	})

	err = theCloud.TcpRoutes().Insert(ctx, tcprKey, tcpr)
	if err != nil {
		t.Fatalf("Insert() = %v", err)
	}

	// Get
	tcpRoute, err := theCloud.TcpRoutes().Get(ctx, tcprKey)
	t.Logf("tcpRoute = %s", pretty.Sprint(tcpRoute))
	if err != nil {
		t.Fatalf("Get(%s) = %v", tcprKey, err)
	}
	serviceName = fmt.Sprintf("https://compute.googleapis.com/v1/projects/%s/global/backendServices/%s", testFlags.project, "bs-after-patch")
	tcpRoute.Rules = append(tcpRoute.Rules, &networkservices.TcpRouteRouteRule{
		Action: &networkservices.TcpRouteRouteAction{
			Destinations: []*networkservices.TcpRouteRouteDestination{
				{ServiceName: serviceName},
			},
		},
	})
	err = theCloud.TcpRoutes().Patch(ctx, tcprKey, tcpRoute)
	if err != nil {
		t.Fatalf("Patch error: %v", err)
	}
	tcpRoute, err = theCloud.TcpRoutes().Get(ctx, tcprKey)
	t.Logf("tcpRoute = %s", pretty.Sprint(tcpRoute))
	if err != nil {
		t.Errorf("After Patch Get(%s) = %v", tcprKey, err)
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

func buildBackendServiceWithNEG(graphBuilder *rgraph.Builder, name string, hcID, negID *cloud.ResourceID) (*cloud.ResourceID, error) {
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

func buildTCPRoute(graphBuilder *rgraph.Builder, name, address, meshURL string, bsID *cloud.ResourceID) (*cloud.ResourceID, error) {
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
						Address: address,
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

type routesServices struct {
	bsID    *cloud.ResourceID
	address string
}

func buildTCPRouteWithBackends(graphBuilder *rgraph.Builder, name, meshURL string, services []routesServices) (*cloud.ResourceID, error) {
	tcpID := tcproute.ID(testFlags.project, meta.GlobalKey(resourceName(name)))
	tcpMutRes := tcproute.NewMutableTcpRoute(testFlags.project, tcpID.Key)

	tcpMutRes.Access(func(x *networkservices.TcpRoute) {
		x.Description = "tcp route for rGraph test"
		x.Name = tcpID.Key.Name
		x.Meshes = []string{meshURL}
		for _, route := range services {
			tcpRoute := networkservices.TcpRouteRouteRule{
				Action: &networkservices.TcpRouteRouteAction{
					Destinations: []*networkservices.TcpRouteRouteDestination{
						{
							ServiceName: resourceSelfLink(route.bsID),
							Weight:      10,
						},
					},
				},
				Matches: []*networkservices.TcpRouteRouteMatch{
					{
						Address: route.address,
						Port:    "80",
					},
				},
			}

			x.Rules = append(x.Rules, &tcpRoute)
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

func ensureMesh(ctx context.Context, t *testing.T) (string, *meta.Key) {
	meshKey := meta.GlobalKey(resourceName(meshName))
	mesh, err := theCloud.Meshes().Get(ctx, meshKey)
	//TODO(kl52752) Add check for NotFoundError
	if err != nil {
		// Mesh not found create one
		meshLocal := networkservices.Mesh{
			Name: resourceName(meshName),
		}
		t.Logf("Insert mesh %v", meshLocal)
		// TODO(kl52752) Fix this this function does not work, error might be in
		// TD adapter
		err = theCloud.Meshes().Insert(ctx, meshKey, &meshLocal)
		if err != nil {
			t.Fatalf("theCloud.Meshes().Insert(_, %v, %+v) = %v, want nil", meshKey, meshLocal, err)
		}
		mesh, err = theCloud.Meshes().Get(ctx, meshKey)
		if err != nil {
			t.Fatalf("theCloud.Meshes().Get(_, %v) = %v, want nil", meshKey, err)
		}
	}
	return mesh.SelfLink, meshKey
}
func TestRgraphLBDelete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	meshURL, meshKey := ensureMesh(ctx, t)
	t.Cleanup(func() {
		err := theCloud.Meshes().Delete(ctx, meshKey)
		t.Logf("bs delete: %v", err)
	})
	graphBuilder := rgraph.NewBuilder()
	negID, err := buildNEG(graphBuilder, negName, zone)
	if err != nil {
		t.Fatalf("buildNEG(_, %s, %s) = (_, %v), want (_, nil)", negName, zone, err)
	}

	hcID, err := buildHealthCheck(graphBuilder, "hc-test", 15)
	if err != nil {
		t.Fatalf("buildHealthCheck(_, hc-test, _) = (_, %v), want (_, nil)", err)
	}
	bsID, err := buildBackendServiceWithNEG(graphBuilder, "bs-test", hcID, negID)
	if err != nil {
		t.Fatalf("buildBackendServiceWithNEG(_, bs-test, _, _) = (_, %v), want (_, nil)", err)
	}

	tcpID, err := buildTCPRoute(graphBuilder, "tcproute-test", routeCIDR, meshURL, bsID)
	if err != nil {
		t.Fatalf("buildBackendServiceWithNEG(_, tcproute-test, _, _, _) = (_, %v), want (_, nil)", err)
	}

	graph, err := graphBuilder.Build()
	if err != nil {
		t.Fatalf("graphBuilder.Build() = %v, want nil", err)
	}

	result, err := plan.Do(ctx, theCloud, graph)
	if err != nil {
		t.Fatalf("plan.Do(_, _, _) = %v, want nil", err)
	}

	ex, err := exec.NewSerialExecutor(result.Actions)
	if err != nil {
		t.Logf("exec.NewSerialExecutor err: %v", err)
		return
	}
	res, err := ex.Run(context.Background(), theCloud)
	if err != nil || res == nil {
		t.Errorf("ex.Run(_,_) = ( %v, %v), want (*result, nil)", res, err)
	}

	bs := graphBuilder.Get(bsID)
	bs.SetState(rnode.NodeDoesNotExist)

	tcpR := graphBuilder.Get(tcpID)
	tcpR.SetState(rnode.NodeDoesNotExist)

	hcR := graphBuilder.Get(hcID)
	hcR.SetState(rnode.NodeDoesNotExist)

	graph, err = graphBuilder.Build()
	if err != nil {
		t.Fatalf("After delete graphBuilder.Build() = %v, want nil", err)
	}

	result, err = plan.Do(ctx, theCloud, graph)
	if err != nil {
		t.Fatalf("After delete plan.Do(_, _, _) = %v, want nil", err)
	}

	if len(result.Actions) == 0 {
		t.Fatalf("len(result.Actions) == 0")
	}
	for _, a := range result.Actions {
		if a.Metadata().Type != exec.ActionTypeDelete {
			t.Logf("Action not delete %+v", a.Metadata())
		} else {
			t.Logf("Found Action delete %+v", a.Metadata())
		}
	}

	ex, err = exec.NewSerialExecutor(result.Actions)
	if err != nil {
		t.Logf("exec.NewSerialExecutor err: %v", err)
		return
	}
	res, err = ex.Run(context.Background(), theCloud)
	if err != nil || res == nil {
		t.Errorf("Delete ex.Run(_,_) = ( %v, %v), want (*result, nil)", res, err)
	}

	t.Logf("exec got Result.Completed len(%d) =\n%v", len(res.Completed), res.Completed)
	t.Logf("exec got Result.Errors len(%d) =\n%v", len(res.Errors), res.Errors)
	t.Logf("exec got Result.Pending len(%d) =\n%v", len(res.Pending), res.Pending)
}

func TestRgraphTCPRouteAddBackends(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	meshURL, meshKey := ensureMesh(ctx, t)
	t.Cleanup(func() {
		err := theCloud.Meshes().Delete(ctx, meshKey)
		t.Logf("bs delete: %v", err)
	})
	graphBuilder := rgraph.NewBuilder()
	negID, err := buildNEG(graphBuilder, negName, zone)
	if err != nil {
		t.Fatalf("buildNEG(_, %s, %s) = (_, %v), want (_, nil)", negName, zone, err)
	}

	hcID, err := buildHealthCheck(graphBuilder, "hc-test", 15)
	if err != nil {
		t.Fatalf("buildHealthCheck(_, hc-test, _) = (_, %v), want (_, nil)", err)
	}
	bsID, err := buildBackendServiceWithNEG(graphBuilder, "bs-test", hcID, negID)
	if err != nil {
		t.Fatalf("buildBackendServiceWithNEG(_, bs-test, _, _) = (_, %v), want (_, nil)", err)
	}

	_, err = buildTCPRoute(graphBuilder, "tcproute-test", routeCIDR, meshURL, bsID)
	if err != nil {
		t.Fatalf("buildTCPRoute(_, tcproute-test, _, _, _) = (_, %v), want (_, nil)", err)
	}

	graph, err := graphBuilder.Build()
	if err != nil {
		t.Fatalf("graphBuilder.Build() = %v, want nil", err)
	}

	result, err := plan.Do(ctx, theCloud, graph)
	if err != nil {
		t.Fatalf("plan.Do(_, _, _) = %v, want nil", err)
	}

	ex, err := exec.NewSerialExecutor(result.Actions)
	if err != nil {
		t.Logf("exec.NewSerialExecutor err: %v", err)
		return
	}
	res, err := ex.Run(context.Background(), theCloud)
	if err != nil || res == nil {
		t.Errorf("ex.Run(_,_) = ( %v, %v), want (*result, nil)", res, err)
	}

	negID2, err := buildNEG(graphBuilder, negName2, zone)
	if err != nil {
		t.Fatalf("buildNEG(_, %s, %s) = (_, %v), want (_, nil)", negName2, zone, err)
	}

	hcID2, err := buildHealthCheck(graphBuilder, "hc-test-2", 15)
	if err != nil {
		t.Fatalf("buildHealthCheck(_, hc-test, _) = (_, %v), want (_, nil)", err)
	}
	bsID2, err := buildBackendServiceWithNEG(graphBuilder, "bs-test-2", hcID2, negID2)
	if err != nil {
		t.Fatalf("buildBackendServiceWithNEG(_, bs-test-2, _, _) = (_, %v), want (_, nil)", err)
	}
	routes := []routesServices{
		{bsID, routeCIDR},
		{bsID2, routeCIRD2},
	}
	_, err = buildTCPRouteWithBackends(graphBuilder, "tcproute-test", meshURL, routes)

	graph, err = graphBuilder.Build()
	if err != nil {
		t.Fatalf("After update graphBuilder.Build() = %v, want nil", err)
	}

	result, err = plan.Do(ctx, theCloud, graph)
	if err != nil {
		t.Fatalf("After update plan.Do(_, _, _) = %v, want nil", err)
	}

	if len(result.Actions) == 0 {
		t.Fatalf("len(result.Actions) == 0")
	}
	t.Log("Planing after add:")
	t.Logf("results.Action: %v", result.Actions)
	for _, a := range result.Actions {
		if a.Metadata().Type != exec.ActionTypeUpdate {
			t.Logf("Action not update %+v", a.Metadata())
		} else {
			t.Logf("Found Action update %+v", a.Metadata())
		}
	}

	ex, err = exec.NewSerialExecutor(result.Actions)
	if err != nil {
		t.Logf("exec.NewSerialExecutor err: %v", err)
		return
	}
	res, err = ex.Run(context.Background(), theCloud)
	if err != nil || res == nil {
		t.Errorf("Delete ex.Run(_,_) = ( %v, %v), want (*result, nil)", res, err)
	}

	t.Logf("exec got Result.Completed len(%d) =\n%v", len(res.Completed), res.Completed)
	t.Logf("exec got Result.Errors len(%d) =\n%v", len(res.Errors), res.Errors)
	t.Logf("exec got Result.Pending len(%d) =\n%v", len(res.Pending), res.Pending)
}
