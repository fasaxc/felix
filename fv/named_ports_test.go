// +build fvtests

// Copyright (c) 2017 Tigera, Inc. All rights reserved.
//
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

package fv_test

import (
	"strconv"

	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	log "github.com/sirupsen/logrus"

	"github.com/projectcalico/felix/fv/containers"
	"github.com/projectcalico/felix/fv/utils"
	"github.com/projectcalico/felix/fv/workload"
	"github.com/projectcalico/libcalico-go/lib/api"
	"github.com/projectcalico/libcalico-go/lib/client"
	"github.com/projectcalico/libcalico-go/lib/numorstring"
	. "github.com/onsi/ginkgo/extensions/table"
)

var HaveConnectivityToPort = workload.HaveConnectivityToPort

var _ = FContext("Named ports: with initialized Felix, etcd datastore, 3 workloads, allow-all profile", func() {

	var (
		etcd   *containers.Container
		felix  *containers.Container
		client *client.Client
		w      [3]*workload.Workload
	)

	const (
		sharedPortName = "shared-tcp"
		sharedPort     = 1100
		w0Port         = 1000
		w1Port         = 1001
		w2Port         = 1002
	)

	BeforeEach(func() {

		etcd = RunEtcd()

		felix = RunFelix(etcd.IP)

		client = GetEtcdClient(etcd.IP)
		err := client.EnsureInitialized()
		Expect(err).NotTo(HaveOccurred())

		felixNode := api.NewNode()
		felixNode.Metadata.Name = felix.Hostname
		_, err = client.Nodes().Create(felixNode)
		Expect(err).NotTo(HaveOccurred())

		// Install a default profile that allows workloads with this profile to talk to each
		// other, in the absence of any Policy.
		defaultProfile := api.NewProfile()
		defaultProfile.Metadata.Name = "default"
		defaultProfile.Metadata.Tags = []string{"default"}
		defaultProfile.Spec.EgressRules = []api.Rule{{Action: "allow"}}
		defaultProfile.Spec.IngressRules = []api.Rule{{
			Action: "allow",
			Source: api.EntityRule{Tag: "default"},
		}}
		_, err = client.Profiles().Create(defaultProfile)
		Expect(err).NotTo(HaveOccurred())

		// Create three workloads, using that profile.
		for ii := 0; ii < 3; ii++ {
			iiStr := strconv.Itoa(ii)
			workloadTCPPort := uint16(1000 + ii)
			w[ii] = workload.Run(
				felix,
				"w"+iiStr,
				"cali1"+iiStr,
				"10.65.0.1"+iiStr,
				fmt.Sprintf("3000,4000,1100,%d", workloadTCPPort),
			)

			w[ii].WorkloadEndpoint.Spec.Ports = []api.EndpointPort{
				{
					Port:     sharedPort,
					Name:     sharedPortName,
					Protocol: numorstring.ProtocolFromString("tcp"),
				},
				{
					Port:     workloadTCPPort,
					Name:     fmt.Sprintf("w%d-tcp", ii),
					Protocol: numorstring.ProtocolFromString("tcp"),
				},
				{
					Port:     2200,
					Name:     "shared-udp",
					Protocol: numorstring.ProtocolFromString("udp"),
				},
				{
					Port:     uint16(1000 + ii),
					Name:     fmt.Sprintf("w%d-udp", ii),
					Protocol: numorstring.ProtocolFromString("udp"),
				},
			}
			w[ii].Configure(client)
		}
	})

	AfterEach(func() {

		if CurrentGinkgoTestDescription().Failed {
			log.Warn("Test failed, dumping diags...")
			utils.Run("docker", "logs", felix.Name)
			utils.Run("docker", "exec", felix.Name, "iptables-save", "-c")
			utils.Run("docker", "exec", felix.Name, "ipset", "list")
			utils.Run("docker", "exec", felix.Name, "ip", "r")
		}

		for ii := 0; ii < 3; ii++ {
			w[ii].Stop()
		}
		felix.Stop()

		if CurrentGinkgoTestDescription().Failed {
			utils.Run("docker", "exec", etcd.Name, "etcdctl", "ls", "--recursive", "/")
		}
		etcd.Stop()
	})

	type ingressEgress int
	const (
		ingress ingressEgress = iota
		egress
	)

	// Baseline test with no named ports policy.
	Context("with no named port policy", func() {
		It("should give full connectivity to and from workload 0", func() {
			// Outbound, w0 should be able to reach all ports on w1 & w2
			Eventually(w[0]).Should(HaveConnectivityToPort(w[1], sharedPort))
			Eventually(w[0]).Should(HaveConnectivityToPort(w[2], sharedPort))
			Eventually(w[0]).Should(HaveConnectivityToPort(w[1], w1Port))
			Eventually(w[0]).Should(HaveConnectivityToPort(w[2], w2Port))

			// Inbound, w1 and w2 should be able to reach all ports on w0.
			Eventually(w[1]).Should(HaveConnectivityToPort(w[0], sharedPort))
			Eventually(w[2]).Should(HaveConnectivityToPort(w[0], sharedPort))
			Eventually(w[1]).Should(HaveConnectivityToPort(w[0], w0Port))
			Eventually(w[2]).Should(HaveConnectivityToPort(w[0], w0Port))
		})
	})

	createPolicy := func(policy *api.Policy) {
		_, err := client.Policies().Create(policy)
		Expect(err).NotTo(HaveOccurred())
	}

	buildAllowToSharedPortPolicy := func(ie ingressEgress, numNumericPorts int) *api.Policy{
		protoTCP := numorstring.ProtocolFromString("tcp")
		policy := api.NewPolicy()
		policy.Metadata.Name = "policy-1"
		ports := []numorstring.Port{
			numorstring.NamedPort(sharedPortName),
		}
		for i := 0; i < numNumericPorts; i++ {
			ports = append(ports, numorstring.SinglePort(3000+uint16(i)))
		}
		entRule := api.EntityRule{
			Ports: ports,
		}
		apiRule := api.Rule{
		Action:      "allow",
			Protocol:    &protoTCP,
			Destination: entRule,
		}
		rules := []api.Rule{
			apiRule,
		}
		if ie == ingress {
			// Ingress rules, apply only to w[0].
			policy.Spec.IngressRules = rules
			policy.Spec.Selector = w[0].NameSelector()
		} else {
			// Egress rules, to get same result, apply everywhere but w[0].
			policy.Spec.EgressRules = rules
			policy.Spec.Selector = fmt.Sprintf("!(%s)", w[0].NameSelector())
		}
		return policy
	}

	FDescribeTable("with a named port policy allowing only traffic to the shared port",
		func(ie ingressEgress, numNumericPorts int) {
			pol := buildAllowToSharedPortPolicy(ie, numNumericPorts)
			createPolicy(pol)

			// Inbound to w0Port should now be blocked.
			Eventually(w[1]).ShouldNot(HaveConnectivityToPort(w[0], w0Port))
			Eventually(w[2]).ShouldNot(HaveConnectivityToPort(w[0], w0Port))

			// Inbound to numeric should now be blocked.
			Eventually(w[1]).ShouldNot(HaveConnectivityToPort(w[0], 4000))
			Eventually(w[2]).ShouldNot(HaveConnectivityToPort(w[0], 4000))

			// Outbound, w0 should be able to reach all ports on w1 & w2
			Eventually(w[0]).Should(HaveConnectivityToPort(w[1], sharedPort))
			Eventually(w[0]).Should(HaveConnectivityToPort(w[2], sharedPort))
			Eventually(w[0]).Should(HaveConnectivityToPort(w[1], w1Port))
			Eventually(w[0]).Should(HaveConnectivityToPort(w[2], w2Port))

			// Inbound, w1 and w2 should be able to reach the shared port on w0.
			Eventually(w[1]).Should(HaveConnectivityToPort(w[0], sharedPort))
			Eventually(w[2]).Should(HaveConnectivityToPort(w[0], sharedPort))
		},

		Entry("ingress, no-numeric", ingress, 0),
		Entry("egress, no-numeric", egress, 0),
		// Adding a numeric port changes the way we render iptables rules to use blocks.
		Entry("ingress, 1 numeric", ingress, 1),
		Entry("egress, 1 numeric", egress, 1),
		// Adding >15 numeric ports requires more than one block.
		Entry("ingress, 16 numeric", ingress, 16),
		Entry("egress, 16 numeric", egress, 16),
	)

	Context("with a named port ingress policy not allowing traffic to the shared port", func() {
		BeforeEach(func() {
			protoTCP := numorstring.ProtocolFromString("tcp")
			policy := api.NewPolicy()
			policy.Metadata.Name = "policy-1"
			policy.Spec.IngressRules = []api.Rule{
				{
					Action:   "allow",
					Protocol: &protoTCP,
					Destination: api.EntityRule{
						NotPorts: []numorstring.Port{
							numorstring.NamedPort(sharedPortName),
						},
					},
				},
			}
			policy.Spec.Selector = w[0].NameSelector()
			_, err := client.Policies().Create(policy)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should give connectivity only to the shared port on w0", func() {
			// Inbound to w0Port should still be allowed.
			Eventually(w[1]).Should(HaveConnectivityToPort(w[0], w0Port))
			Eventually(w[2]).Should(HaveConnectivityToPort(w[0], w0Port))

			// Outbound, w0 should be able to reach all ports on w1 & w2
			Eventually(w[0]).Should(HaveConnectivityToPort(w[1], sharedPort))
			Eventually(w[0]).Should(HaveConnectivityToPort(w[2], sharedPort))
			Eventually(w[0]).Should(HaveConnectivityToPort(w[1], w1Port))
			Eventually(w[0]).Should(HaveConnectivityToPort(w[2], w2Port))

			// Inbound, w1 and w2 should not be able to reach the shared port on w0.
			Eventually(w[1]).ShouldNot(HaveConnectivityToPort(w[0], sharedPort))
			Eventually(w[2]).ShouldNot(HaveConnectivityToPort(w[0], sharedPort))
		})
	})

	Context("with a named port egress policy blocking traffic to the shared port", func() {
		BeforeEach(func() {
			protoTCP := numorstring.ProtocolFromString("tcp")
			policy := api.NewPolicy()
			policy.Metadata.Name = "policy-1"
			policy.Spec.EgressRules = []api.Rule{
				{
					Action:   "drop",
					Protocol: &protoTCP,
					Destination: api.EntityRule{
						Ports: []numorstring.Port{
							numorstring.NamedPort(sharedPortName),
						},
					},
				},
			}
			policy.Spec.Selector = w[0].NameSelector()
			_, err := client.Policies().Create(policy)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should give connectivity only to the shared port on w0", func() {
			// Inbound to w0Port should still be allowed.
			Eventually(w[1]).Should(HaveConnectivityToPort(w[0], w0Port))
			Eventually(w[2]).Should(HaveConnectivityToPort(w[0], w0Port))

			// Outbound, w0 should be able to reach all ports on w1 & w2
			Eventually(w[0]).Should(HaveConnectivityToPort(w[1], sharedPort))
			Eventually(w[0]).Should(HaveConnectivityToPort(w[2], sharedPort))
			Eventually(w[0]).Should(HaveConnectivityToPort(w[1], w1Port))
			Eventually(w[0]).Should(HaveConnectivityToPort(w[2], w2Port))

			// Inbound, w1 and w2 should not be able to reach the shared port on w0.
			Eventually(w[1]).ShouldNot(HaveConnectivityToPort(w[0], sharedPort))
			Eventually(w[2]).ShouldNot(HaveConnectivityToPort(w[0], sharedPort))
		})
	})
})
