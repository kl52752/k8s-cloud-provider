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

package forwardingrule

import (
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/api"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	alpha "google.golang.org/api/compute/v0.alpha"
	beta "google.golang.org/api/compute/v0.beta"
	"google.golang.org/api/compute/v1"
)

// https://cloud.google.com/compute/docs/reference/rest/beta/forwardingRules
type typeTrait struct {
	api.BaseTypeTrait[compute.ForwardingRule, alpha.ForwardingRule, beta.ForwardingRule]
}

func (*typeTrait) FieldTraits(meta.Version) *api.FieldTraits {
	dt := api.NewFieldTraits()

	dt.OutputOnly(api.Path{}.Pointer().Field("BaseForwardingRule"))
	dt.OutputOnly(api.Path{}.Pointer().Field("CreationTimestamp"))
	dt.OutputOnly(api.Path{}.Pointer().Field("Fingerprint"))
	dt.OutputOnly(api.Path{}.Pointer().Field("Id"))
	dt.OutputOnly(api.Path{}.Pointer().Field("Kind"))
	dt.OutputOnly(api.Path{}.Pointer().Field("LabelFingerprint"))
	dt.OutputOnly(api.Path{}.Pointer().Field("PscConnectionId"))
	dt.OutputOnly(api.Path{}.Pointer().Field("PscConnectionStatus")) // Not documented
	dt.OutputOnly(api.Path{}.Pointer().Field("Region"))
	dt.OutputOnly(api.Path{}.Pointer().Field("SelfLink"))
	dt.OutputOnly(api.Path{}.Pointer().Field("ServiceName"))

	// TODO: handle alpha/beta

	return dt
}
