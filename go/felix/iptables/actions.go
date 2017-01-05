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

package iptables

import "fmt"

type Action interface {
	ToFragment() string
}

type GotoAction struct {
	Target string
}

func (g GotoAction) ToFragment() string {
	return "--goto " + g.Target
}

type JumpAction struct {
	Target string
}

func (g JumpAction) ToFragment() string {
	return "--jump " + g.Target
}

type ReturnAction struct{}

func (r ReturnAction) ToFragment() string {
	return "--jump RETURN"
}

type DropAction struct{}

func (g DropAction) ToFragment() string {
	return "--jump DROP"
}

type LogAction struct {
	Prefix string
}

func (g LogAction) ToFragment() string {
	return fmt.Sprintf(`--jump LOG --log-prefix "%s: " --log-level 5`, g.Prefix)
}

type AcceptAction struct{}

func (g AcceptAction) ToFragment() string {
	return "--jump ACCEPT"
}

type DNATAction struct {
	DestAddr string
	DestPort uint16
}

func (g DNATAction) ToFragment() string {
	return fmt.Sprintf("--jump DNAT --to-destination %s:%d", g.DestAddr, g.DestPort)
}

type MasqAction struct{}

func (g MasqAction) ToFragment() string {
	return "--jump MASQUERADE"
}

type ClearMarkAction struct {
	Mark uint32
}

func (c ClearMarkAction) ToFragment() string {
	return fmt.Sprintf("--jump MARK --set-mark 0/%#x", c.Mark)
}

type SetMarkAction struct {
	Mark uint32
}

func (c SetMarkAction) ToFragment() string {
	return fmt.Sprintf("--jump MARK --set-mark %#x/%#x", c.Mark, c.Mark)
}