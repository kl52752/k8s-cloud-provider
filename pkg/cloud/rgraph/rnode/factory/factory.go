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
	"fmt"
	"reflect"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/rgraph/rnode"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/rgraph/rnode/healthcheck"
	alpha "google.golang.org/api/compute/v0.alpha"
	beta "google.golang.org/api/compute/v0.beta"
	"google.golang.org/api/compute/v1"
)

const (
	healthCheckService = "healthChecks"
)

// Factory is a
type Factory struct {
	projects cloud.ProjectRouter
	versions cloud.VersionResolver
}

// NewFactory
func NewFactory(pr cloud.ProjectRouter, vr cloud.VersionResolver) *Factory {
	return &Factory{
		projects: pr,
		versions: vr,
	}
}

type resourceMeta struct {
	Project string
	Version meta.Version
	Scope   meta.Scope
}

func (b *resourceMeta) Key(name, location string) *meta.Key {
	switch b.Scope {
	case meta.GlobalScope:
		return meta.GlobalKey(name)
	case meta.RegionalScope:
		return meta.RegionalKey(name, location)
	case meta.ZonalScope:
		return meta.ZonalKey(name, location)
	}
	// should never happened
	return meta.GlobalKey(name)
}

// HealthCheck creates factory with initialised resource meta
func (f *Factory) HealthCheck(ctx context.Context, scope meta.Scope) *HealthCheckFactory {
	ver := f.versions.Version(cloud.VersionResolverKey{Service: healthCheckService, Scope: scope})
	project := f.projects.ProjectID(ctx, ver, healthCheckService)

	return &HealthCheckFactory{
		resourceMeta{
			Project: project,
			Version: ver,
			Scope:   scope,
		},
	}
}

type HealthCheckFactory struct {
	resourceMeta
}

// extractResource converts the any object and get's it's Name and Interface.
// This function expect that any is a struct.
func extractResource(res any) (string, any) {
	v := reflect.ValueOf(res)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return "", nil
	}
	return v.FieldByName("Name").String(), v.Interface()
}

// CreateBuilderGeneric will get resource version from VersionResolver.
// Object type will be deduced based on the version.
// Error is returned if object type does not match expected version.
// There might be only 1 set version per object and scope for factory.
func (b *HealthCheckFactory) CreateBuilderGeneric(hc any, state rnode.NodeState, location string) (*rnode.Builder, error) {

	hcName, hcInt := extractResource(hc)
	if hcName == "" {
		return nil, fmt.Errorf("Resource does not have a Name: %v", hc)
	}

	m := healthcheck.NewMutableHealthCheck(b.Project, b.Key(hcName, location))
	switch b.Version {

	case meta.VersionGA:
		gaHC, ok := hcInt.(compute.HealthCheck)
		if !ok {
			return nil, fmt.Errorf("Health check not convertible to compute.HealthCheck: %T", hc)
		}
		f := func(x *compute.HealthCheck) {
			*x = gaHC
		}
		m.Access(f)
	case meta.VersionAlpha:
		alphaHC, ok := hcInt.(*alpha.HealthCheck)
		if !ok {
			return nil, fmt.Errorf("Healthcheck not convertible to compute.HealthCheck")
		}
		f := func(x *alpha.HealthCheck) {
			*x = *alphaHC
		}
		m.AccessAlpha(f)
	case meta.VersionBeta:
		betaHC, ok := hcInt.(*beta.HealthCheck)
		if !ok {
			return nil, fmt.Errorf("Healthcheck not convertible to compute.HealthCheck")
		}
		f := func(x *beta.HealthCheck) {
			*x = *betaHC
		}
		m.AccessBeta(f)
	}

	r, _ := m.Freeze()
	nb := healthcheck.NewBuilderWithResource(r)
	nb.SetOwnership(rnode.OwnershipManaged)
	nb.SetState(state)
	return &nb, nil
}

// Each version has separate function.
// Pros less complex, easier to maintain, no need for error checking and validation at this step
func (b *HealthCheckFactory) CreateBuilderGA(hc compute.HealthCheck, state rnode.NodeState, location string) *rnode.Builder {
	b.Version = meta.VersionGA
	m := healthcheck.NewMutableHealthCheck(b.Project, b.Key(hc.Name, location))
	f := func(x *compute.HealthCheck) {
		*x = hc
	}
	m.Access(f)

	r, _ := m.Freeze()
	nb := healthcheck.NewBuilderWithResource(r)
	nb.SetOwnership(rnode.OwnershipManaged)
	nb.SetState(state)
	return &nb
}

func (b *HealthCheckFactory) CreateBuilderAlpha(hc alpha.HealthCheck, state rnode.NodeState, location string) *rnode.Builder {
	b.Version = meta.VersionAlpha
	m := healthcheck.NewMutableHealthCheck(b.Project, b.Key(hc.Name, location))
	f := func(x *alpha.HealthCheck) {
		*x = hc
	}
	m.AccessAlpha(f)

	r, _ := m.Freeze()
	nb := healthcheck.NewBuilderWithResource(r)
	nb.SetOwnership(rnode.OwnershipManaged)
	nb.SetState(state)
	return &nb
}

func (b *HealthCheckFactory) CreateBuilderBeta(hc beta.HealthCheck, state rnode.NodeState, location string) *rnode.Builder {
	b.Version = meta.VersionBeta
	m := healthcheck.NewMutableHealthCheck(b.Project, b.Key(hc.Name, location))
	f := func(x *beta.HealthCheck) {
		*x = hc
	}
	m.AccessBeta(f)

	r, _ := m.Freeze()
	nb := healthcheck.NewBuilderWithResource(r)
	nb.SetOwnership(rnode.OwnershipManaged)
	nb.SetState(state)
	return &nb
}
