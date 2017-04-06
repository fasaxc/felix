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

package dispatcher

import (
	"reflect"

	log "github.com/Sirupsen/logrus"

	"github.com/projectcalico/felix/epkey"
	"github.com/projectcalico/libcalico-go/lib/backend/api"
	"github.com/projectcalico/libcalico-go/lib/backend/model"
)

// Update is our own version of libcalico's Update struct, in our version we allow interface{}
// for the Key type so that we can use non-model keys.
type Update struct {
	Key        interface{}
	Value      interface{}
	UpdateType api.UpdateType
}

func UpdateFromAPIUpdate(u api.Update) Update {
	var k interface{} = u.Key
	switch typedKey := k.(type) {
	case model.WorkloadEndpointKey:
		k = epkey.FromWorkloadKey(typedKey)
	case model.HostEndpointKey:
		k = epkey.FromHostKey(typedKey)
	}
	return Update{
		Key:        k,
		Value:      u.Value,
		UpdateType: u.UpdateType,
	}
}

type UpdateHandler func(update Update) (filterOut bool)

type StatusHandler func(status api.SyncStatus)

// Dispatcher fans out incoming events based on their reflect.Type.  One or more UpdateHandler
// functions can be registered for each type.
//
// The Dispatcher supports rudimentary filtering:  UpdateHandlers are called in the order
// they were added.  Earlier handlers can return filterOut=true to prevent further handlers
// being called.
type Dispatcher struct {
	typeToHandler  map[reflect.Type]updateHandlers
	statusHandlers []StatusHandler
}

type updateHandlers []UpdateHandler

func (u updateHandlers) DispatchToAll(update Update) {
	for _, onUpdate := range u {
		filterOut := onUpdate(update)
		if filterOut {
			// Note: we don't propagate the filterOut flag.  We only
			// filter downstream in the processing pipeline, we don't
			// want to prevent our peers from handling updates.
			break
		}
	}
}

// NewDispatcher creates a Dispatcher with all its event handlers set to no-ops.
func NewDispatcher() *Dispatcher {
	d := &Dispatcher{
		typeToHandler: make(map[reflect.Type]updateHandlers),
	}
	return d
}

func (d *Dispatcher) Register(keyExample interface{}, receiver UpdateHandler) {
	keyType := reflect.TypeOf(keyExample)
	if keyType.Kind() == reflect.Ptr {
		panic("Register expects a non-pointer")
	}
	log.Infof("Registering listener for type %v: %#v", keyType, receiver)
	d.typeToHandler[keyType] = append(d.typeToHandler[keyType], receiver)
}

func (d *Dispatcher) RegisterStatusHandler(handler StatusHandler) {
	d.statusHandlers = append(d.statusHandlers, handler)
}

// Syncer callbacks.

func (d *Dispatcher) OnUpdates(updates []Update) {
	for _, update := range updates {
		d.OnUpdate(update)
	}
}

func (d *Dispatcher) OnStatusUpdated(status api.SyncStatus) {
	for _, onStatusUpdate := range d.statusHandlers {
		onStatusUpdate(status)
	}
}

// Dispatcher callbacks.

func (d *Dispatcher) OnUpdate(update Update) (filterOut bool) {
	log.Debugf("Dispatching %v", update)
	keyType := reflect.TypeOf(update.Key)
	log.Debug("Type: ", keyType)
	if update.Value != nil && reflect.TypeOf(update.Value).Kind() == reflect.Struct {
		log.Fatalf("KVPair contained a struct instead of expected pointer: %#v", update)
	}
	typeSpecificHandlers := d.typeToHandler[keyType]
	log.WithField("typeSpecificHandlers", typeSpecificHandlers).Debug(
		"Looked up type-specific handlers")
	typeSpecificHandlers.DispatchToAll(update)
	return
}

func (d *Dispatcher) OnDatamodelStatus(status api.SyncStatus) {
	d.OnStatusUpdated(status)
}
