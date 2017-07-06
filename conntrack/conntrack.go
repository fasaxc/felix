// Copyright (c) 2016-2017 Tigera, Inc. All rights reserved.
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

package conntrack

import (
	"net"
	"os/exec"

	log "github.com/Sirupsen/logrus"
	"github.com/vishvananda/netlink"
)

var conntrackDirections = []string{
	"--orig-src",
	"--orig-dst",
	"--reply-src",
	"--reply-dst",
}

const numRetries = 3

type Conntrack struct {
	newCmd newCmd
}

func New() *Conntrack {
	return NewWithCmdShim(func(name string, arg ...string) CmdIface {
		return exec.Command(name, arg...)
	})
}

// NewWithCmdShim is a test constructor that allows for shimming exec.Command.
func NewWithCmdShim(newCmd newCmd) *Conntrack {
	return &Conntrack{
		newCmd: newCmd,
	}
}

type newCmd func(name string, arg ...string) CmdIface

type CmdIface interface {
	CombinedOutput() ([]byte, error)
}

func (c Conntrack) RemoveConntrackFlows(ipVersion uint8, ipAddr net.IP) {
	var family netlink.InetFamily
	switch ipVersion {
	case 4:
		family = netlink.FAMILY_V4
	case 6:
		family = netlink.FAMILY_V6
	default:
		log.WithField("version", ipVersion).Panic("Unknown IP version")
	}

	filter := AnyIPConntrackFilter{
		ip: ipAddr,
	}
	_, err := netlink.ConntrackDeleteFilter(netlink.ConntrackTable, family, &filter)
	if err != nil {
		log.WithError(err).Warn("Failed to delete conntrack flows, ignoring")
	}
	_, err = netlink.ConntrackDeleteFilter(netlink.ConntrackExpectTable, family, &filter)
	if err != nil {
		log.WithError(err).Warn("Failed to delete conntrack expect flows, ignoring")
	}
}

type AnyIPConntrackFilter struct {
	ip net.IP
}

func (f *AnyIPConntrackFilter) MatchConntrackFlow(flow *netlink.ConntrackFlow) bool {
	return f.ip.Equal(flow.Forward.SrcIP) || f.ip.Equal(flow.Forward.DstIP) ||
		f.ip.Equal(flow.Reverse.SrcIP) || f.ip.Equal(flow.Reverse.DstIP)
}
