// Copyright (c) 2017 Tigera, Inc. All rights reserved.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package epkey_test

import (
	. "github.com/projectcalico/felix/epkey"

	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"

	"github.com/projectcalico/libcalico-go/lib/backend/model"
)

var _ = DescribeTable("EndpointKey workload round trip tests",
	func(key model.WorkloadEndpointKey) {
		epKey := FromWorkloadKey(key)
		Expect(epKey.ToModelKey()).To(Equal(key))
		Expect(epKey.Hostname()).To(Equal(key.Hostname))
	},
	Entry("Normal", model.WorkloadEndpointKey{
		Hostname:       "host",
		OrchestratorID: "orch",
		WorkloadID:     "wload",
		EndpointID:     "endpoint"}),
	Entry("k8s", model.WorkloadEndpointKey{
		Hostname:       "host",
		OrchestratorID: "k8s",
		WorkloadID:     "wload",
		EndpointID:     "eth0"}),
	Entry("openstack", model.WorkloadEndpointKey{
		Hostname:       "host",
		OrchestratorID: "openstack",
		WorkloadID:     "wload",
		EndpointID:     "endpoint"}),
)

var _ = DescribeTable("EndpointKey host round trip tests",
	func(key model.HostEndpointKey) {
		epKey := FromHostKey(key)
		Expect(epKey.ToModelKey()).To(Equal(key))
		Expect(epKey.Hostname()).To(Equal(key.Hostname))
	},
	Entry("Normal", model.HostEndpointKey{
		Hostname:   "host",
		EndpointID: "endpoint"}),
)
