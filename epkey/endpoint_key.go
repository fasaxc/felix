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

package epkey

import (
	log "github.com/Sirupsen/logrus"

	"fmt"
	"strings"

	"github.com/projectcalico/libcalico-go/lib/backend/model"
)

type EndpointKey string

const (
	fmtWload          byte = 'n'
	fmtWloadK8sEth0        = 'k'
	fmtWloadOpenStack      = 'o'
	fmtHost                = 'h'
)

func (k EndpointKey) Hostname() string {
	return string(k)[1:strings.Index(string(k), "/")]
}

func (k EndpointKey) IsHostEndpoint() bool {
	if k[0] == fmtHost {
		return true
	}
	return false
}

func (k EndpointKey) ToModelKey() model.Key {
	parts := strings.Split(string(k)[1:], "/")
	switch k[0] {
	case fmtWload:
		return model.WorkloadEndpointKey{
			Hostname:       parts[0],
			OrchestratorID: parts[1],
			WorkloadID:     parts[2],
			EndpointID:     parts[3],
		}
	case fmtWloadK8sEth0:
		return model.WorkloadEndpointKey{
			Hostname:       parts[0],
			OrchestratorID: "k8s",
			WorkloadID:     parts[1],
			EndpointID:     "eth0",
		}
	case fmtWloadOpenStack:
		return model.WorkloadEndpointKey{
			Hostname:       parts[0],
			OrchestratorID: "openstack",
			WorkloadID:     parts[1],
			EndpointID:     parts[2],
		}
	case fmtHost:
		return model.HostEndpointKey{
			Hostname:   parts[0],
			EndpointID: parts[1],
		}
	}
	log.WithField("key", string(k)).Panic("Unknown compressed key type")
	return nil
}

func FromWorkloadKey(key model.WorkloadEndpointKey) EndpointKey {
	if key.OrchestratorID == "k8s" && key.EndpointID == "eth0" {
		// k8s uses eth0 as the hard-coded endpoint ID.
		return EndpointKey(fmt.Sprintf("%c%s/%s",
			fmtWloadK8sEth0,
			key.Hostname,
			key.WorkloadID))
	} else if key.OrchestratorID == "openstack" {
		return EndpointKey(fmt.Sprintf("%c%s/%s/%s",
			fmtWloadOpenStack,
			key.Hostname,
			key.WorkloadID,
			key.EndpointID))
	}
	return EndpointKey(fmt.Sprintf("%c%s/%s/%s/%s",
		fmtWload,
		key.Hostname,
		key.OrchestratorID,
		key.WorkloadID,
		key.EndpointID))
}

func FromHostKey(key model.HostEndpointKey) EndpointKey {
	return EndpointKey(fmt.Sprintf("%c%s/%s",
		fmtHost,
		key.Hostname,
		key.EndpointID))
}
