// Copyright (c) 2019-2020 Tigera, Inc. All rights reserved.
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

package intdataplane

import (
	"time"

	"github.com/projectcalico/felix/bpf/ipsets"

	"github.com/projectcalico/felix/idalloc"

	log "github.com/sirupsen/logrus"

	"github.com/projectcalico/felix/bpf"
	"github.com/projectcalico/felix/proto"
	"github.com/projectcalico/libcalico-go/lib/set"
)

type bpfIPSetManager struct {
	// ipSets contains an entry for each IP set containing the state of that IP set.
	ipSets map[uint64]*bpfIPSet

	ipSetIDAllocator *idalloc.IDAllocator

	bpfMap bpf.Map

	dirtyIPSetIDs   set.Set
	resyncScheduled bool
}

func newBPFIPSetManager(ipSetIDAllocator *idalloc.IDAllocator, ipSetsMap bpf.Map) *bpfIPSetManager {
	return &bpfIPSetManager{
		ipSets:           map[uint64]*bpfIPSet{},
		dirtyIPSetIDs:    set.New(), /*set entries are uint64 IDs */
		bpfMap:           ipSetsMap,
		resyncScheduled:  true,
		ipSetIDAllocator: ipSetIDAllocator,
	}
}

func (m *bpfIPSetManager) OnUpdate(msg interface{}) {
	switch msg := msg.(type) {
	// IP set-related messages.
	case *proto.IPSetDeltaUpdate:
		// Deltas are extremely common: IPs being added and removed as workloads come and go.
		m.onIPSetDeltaUpdate(msg)
	case *proto.IPSetUpdate:
		// Updates are usually only seen when a new IP set is created.
		m.onIPSetUpdate(msg)
	case *proto.IPSetRemove:
		m.onIPSetRemove(msg)
	}
}

// getExistingIPSetString gets the IP set data given the string set ID; returns nil if the IP set wasn't present.
// Never allocates an IP set ID from the allocator.
func (m *bpfIPSetManager) getExistingIPSetString(setID string) *bpfIPSet {
	id := m.ipSetIDAllocator.GetNoAlloc(setID)
	if id == 0 {
		return nil
	}
	return m.ipSets[id]
}

// getExistingIPSet gets the IP set data given the uint64 ID; returns nil if the IP set wasn't present.
// Never allocates an IP set ID from the allocator.
func (m *bpfIPSetManager) getExistingIPSet(setID uint64) *bpfIPSet {
	return m.ipSets[setID]
}

// getOrCreateIPSet gets the IP set data given the string set ID; allocates a new uint64 ID and creates the tracking
// struct if needed.  The returned struct will never have Deleted=true.
//
// Call deleteIPSetAndReleaseID to release the ID again and discard the tracking struct.
func (m *bpfIPSetManager) getOrCreateIPSet(setID string) *bpfIPSet {
	id := m.ipSetIDAllocator.GetOrAlloc(setID)
	ipSet := m.ipSets[id]
	if ipSet == nil {
		ipSet = &bpfIPSet{
			ID:             id,
			OriginalID:     setID,
			DesiredEntries: set.New(),
			PendingAdds:    set.New(),
			PendingRemoves: set.New(),
		}
		m.ipSets[id] = ipSet
	} else {
		// Possible that this IP set was queued for deletion but it just got recreated.
		ipSet.Deleted = false
	}
	return ipSet
}

// deleteIPSetAndReleaseID deleted the IP set tracking struct from the map and releases the ID.
func (m *bpfIPSetManager) deleteIPSetAndReleaseID(ipSet *bpfIPSet) {
	delete(m.ipSets, ipSet.ID)
	err := m.ipSetIDAllocator.ReleaseUintID(ipSet.ID)
	if err != nil {
		log.WithField("id", ipSet.ID).WithError(err).Panic("Failed to release IP set UID")
	}
}

func (m *bpfIPSetManager) onIPSetUpdate(msg *proto.IPSetUpdate) {
	ipSet := m.getOrCreateIPSet(msg.Id)
	log.WithFields(log.Fields{"stringID": msg.Id, "uint64ID": ipSet.ID}).Info("IP set added")
	ipSet.ReplaceMembers(msg.Members)
	m.markIPSetDirty(ipSet)
}

func (m *bpfIPSetManager) onIPSetRemove(msg *proto.IPSetRemove) {
	ipSet := m.getExistingIPSetString(msg.Id)
	if ipSet == nil {
		log.WithField("setID", msg.Id).Panic("Received deletion for unknown IP set")
		return
	}
	if ipSet.Deleted {
		log.WithField("setID", msg.Id).Panic("Received deletion for already-deleted IP set")
		return
	}
	ipSet.RemoveAll()
	ipSet.Deleted = true
	m.markIPSetDirty(ipSet)
}

func (m *bpfIPSetManager) onIPSetDeltaUpdate(msg *proto.IPSetDeltaUpdate) {
	ipSet := m.getExistingIPSetString(msg.Id)
	if ipSet == nil {
		log.WithField("setID", msg.Id).Panic("Received delta for unknown IP set")
		return
	}
	if ipSet.Deleted {
		log.WithField("setID", msg.Id).Panic("Received delta for already-deleted IP set")
		return
	}
	log.WithFields(log.Fields{
		"stringID": msg.Id,
		"uint64ID": ipSet.ID,
		"added":    len(msg.AddedMembers),
		"removed":  len(msg.RemovedMembers),
	}).Info("IP delta update")
	for _, member := range msg.RemovedMembers {
		entry := ipsets.ProtoIPSetMemberToBPFEntry(ipSet.ID, member)
		ipSet.RemoveMember(entry)
	}
	for _, member := range msg.AddedMembers {
		entry := ipsets.ProtoIPSetMemberToBPFEntry(ipSet.ID, member)
		ipSet.AddMember(entry)
	}
	m.markIPSetDirty(ipSet)
}

func (m *bpfIPSetManager) CompleteDeferredWork() error {
	var numAdds, numDels uint
	startTime := time.Now()

	err := m.bpfMap.EnsureExists()
	if err != nil {
		log.WithError(err).Panic("Failed to create IP set map")
	}

	debug := log.GetLevel() >= log.DebugLevel
	if m.resyncScheduled {
		log.Info("Doing full resync of BPF IP sets map")
		m.resyncScheduled = false

		m.dirtyIPSetIDs.Clear()

		// Start by configuring every IP set to add all its entries to the dataplane.  Then, as we scan the dataplane,
		// we'll make sure that each gets cleaned up.
		for _, ipSet := range m.ipSets {
			ipSet.PendingAdds = ipSet.DesiredEntries.Copy()
			ipSet.PendingRemoves.Clear()
		}

		err := m.bpfMap.Iter(func(k, v []byte) bpf.IteratorAction {
			var entry ipsets.IPSetEntry
			copy(entry[:], k)
			setID := entry.SetID()
			if debug {
				log.WithFields(log.Fields{"setID": setID,
					"addr":      entry.Addr(),
					"prefixLen": entry.PrefixLen()}).Debug("Found entry in dataplane")
			}
			ipSet := m.ipSets[setID]
			if ipSet == nil {
				log.WithField("entry", entry).Debug("Found entry from unknown IP set. Delete.")
				return bpf.IterDelete
			} else {
				// Entry is from a known IP set.  Check if the entry is wanted.
				if ipSet.DesiredEntries.Contains(entry) {
					log.WithField("entry", entry).Debug("Entry we were about to add, clean up pending set.")
					ipSet.PendingAdds.Discard(entry)
				} else {
					log.WithField("entry", entry).Debug("Entry we don't want. Delete.")
					ipSet.PendingRemoves.Discard(entry)
					return bpf.IterDelete
				}
			}
			return bpf.IterNone
		})
		if err != nil {
			log.WithError(err).Error("Failed to iterate over BPF map; IP sets may be out of sync")
			m.resyncScheduled = true
			return err
		}

		for _, ipSet := range m.ipSets {
			if ipSet.Dirty() {
				m.markIPSetDirty(ipSet)
			}
		}
	}

	m.dirtyIPSetIDs.Iter(func(item interface{}) error {
		setID := item.(uint64)
		leaveDirty := false
		ipSet := m.getExistingIPSet(setID)
		if ipSet == nil {
			log.WithField("id", setID).Warn("Couldn't find IP set that was marked as dirty.")
			m.resyncScheduled = true
			return set.RemoveItem
		}

		ipSet.PendingRemoves.Iter(func(item interface{}) error {
			entry := item.(ipsets.IPSetEntry)
			if debug {
				log.WithFields(log.Fields{"setID": setID, "entry": entry}).Debug("Removing entry from IP set")
			}
			err := m.bpfMap.Delete(entry[:])
			if err != nil {
				log.WithFields(log.Fields{"setID": setID, "entry": entry}).WithError(err).Error("Failed to remove IP set entry")
				leaveDirty = true
				return nil
			}
			numDels++
			return set.RemoveItem
		})

		ipSet.PendingAdds.Iter(func(item interface{}) error {
			entry := item.(ipsets.IPSetEntry)
			if debug {
				log.WithFields(log.Fields{"setID": setID, "entry": entry}).Debug("Adding entry to IP set")
			}
			err := m.bpfMap.Update(entry[:], ipsets.DummyValue)
			if err != nil {
				log.WithFields(log.Fields{"setID": setID, "entry": entry}).WithError(err).Error("Failed to add IP set entry")
				leaveDirty = true
				return nil
			}
			numAdds++
			return set.RemoveItem
		})

		if leaveDirty {
			log.WithField("setID", setID).Debug("IP set still dirty, queueing resync")
			m.resyncScheduled = true
			return nil
		}

		if ipSet.Deleted {
			// Clean and deleted, time to release the IP set ID.
			m.deleteIPSetAndReleaseID(ipSet)
		}

		log.WithField("setID", setID).Debug("IP set is now clean")
		return set.RemoveItem
	})

	duration := time.Since(startTime)
	if numDels > 0 || numAdds > 0 {
		log.WithFields(log.Fields{
			"timeTaken": duration,
			"numAdds":   numAdds,
			"numDels":   numDels,
		}).Info("Completed updates to BPF IP sets.")
	}

	return nil
}

func (m *bpfIPSetManager) markIPSetDirty(data *bpfIPSet) {
	m.dirtyIPSetIDs.Add(data.ID)
}

type bpfIPSet struct {
	OriginalID string
	ID         uint64

	// DesiredEntries contains all the entries that we _want_ to be in the set.
	DesiredEntries set.Set /* of IPSetEntry */
	// PendingAdds contains all the entries that we need to add to bring the dataplane into sync with DesiredEntries.
	PendingAdds set.Set /* of IPSetEntry */
	// PendingRemoves contains all the entries that we need to remove from the dataplane to bring the
	// dataplane into sync with DesiredEntries.
	PendingRemoves set.Set /* of IPSetEntry */

	Deleted bool
}

func (m *bpfIPSet) ReplaceMembers(members []string) {
	m.RemoveAll()
	m.AddMembers(members)
}

func (m *bpfIPSet) RemoveAll() {
	m.DesiredEntries.Iter(func(item interface{}) error {
		entry := item.(ipsets.IPSetEntry)
		m.RemoveMember(entry)
		return nil
	})
}

func (m *bpfIPSet) AddMembers(members []string) {
	for _, member := range members {
		entry := ipsets.ProtoIPSetMemberToBPFEntry(m.ID, member)
		m.AddMember(entry)
	}
}

// AddMember adds a member to the set of desired entries. Idempotent, if the member is already present, makes no change.
func (m *bpfIPSet) AddMember(entry ipsets.IPSetEntry) {
	if m.DesiredEntries.Contains(entry) {
		return
	}
	m.DesiredEntries.Add(entry)
	if m.PendingRemoves.Contains(entry) {
		m.PendingRemoves.Discard(entry)
	} else {
		m.PendingAdds.Add(entry)
	}
}

// RemoveMember removes a member from the set of desired entries. Idempotent, if the member is no present, makes no
// change.
func (m *bpfIPSet) RemoveMember(entry ipsets.IPSetEntry) {
	if !m.DesiredEntries.Contains(entry) {
		return
	}
	m.DesiredEntries.Discard(entry)
	if m.PendingAdds.Contains(entry) {
		m.PendingAdds.Discard(entry)
	} else {
		m.PendingRemoves.Add(entry)
	}
}

func (m *bpfIPSet) Dirty() bool {
	return m.PendingRemoves.Len() > 0 || m.PendingAdds.Len() > 0 || m.Deleted
}
