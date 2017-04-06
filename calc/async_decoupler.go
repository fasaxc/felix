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

package calc

import (
	"github.com/projectcalico/felix/dispatcher"
	"github.com/projectcalico/libcalico-go/lib/backend/api"
)

type Callbacks interface {
	// OnStatusUpdated is called when the status of the sync status of the
	// datastore changes.
	OnStatusUpdated(status api.SyncStatus)

	// OnUpdates is called when the Syncer has one or more updates to report.
	// Updates consist of typed key-value pairs.  The keys are drawn from the
	// backend.model package.  The values are either nil, to indicate a
	// deletion (or failure to parse a value), or a pointer to a value of
	// the associated value type.
	//
	// When a recursive delete is made, deleting many leaf keys, the Syncer
	// generates deletion updates for all the leaf keys.
	OnUpdates(updates []dispatcher.Update)
}

func NewSyncerCallbacksDecoupler() *SyncerCallbacksDecoupler {
	return &SyncerCallbacksDecoupler{
		c: make(chan interface{}),
	}
}

type SyncerCallbacksDecoupler struct {
	c chan interface{}
}

func (a *SyncerCallbacksDecoupler) OnStatusUpdated(status api.SyncStatus) {
	a.c <- status
}

func (a *SyncerCallbacksDecoupler) OnUpdates(updates []api.Update) {
	a.c <- updates
}

func (a *SyncerCallbacksDecoupler) SendTo(sink Callbacks) {
	for obj := range a.c {
		switch obj := obj.(type) {
		case api.SyncStatus:
			sink.OnStatusUpdated(obj)
		case []api.Update:
			// Convert the updates to our format.
			updates := make([]dispatcher.Update, len(obj))
			for i, u := range obj {
				updates[i] = dispatcher.UpdateFromAPIUpdate(u)
			}
			sink.OnUpdates(updates)
		}
	}
}
