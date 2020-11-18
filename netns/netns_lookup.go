// Copyright (c) 2020 Tigera, Inc. All rights reserved.
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

package netns

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"regexp"
	"syscall"

	log "github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

var ErrNotFound = errors.New("network namespace not found")

type CachedNetNSList struct {
	idToNetNS map[int]NetNS
}

func (c *CachedNetNSList) NetNSHandleByID(id int) (netns.NsHandle, error) {
	if ns, ok := c.idToNetNS[id]; ok {
		return ns.Handle(), nil
	}
	return 0, ErrNotFound
}

func (c *CachedNetNSList) Close() error {
	for _, ns := range c.idToNetNS {
		err := ns.Close()
		if err != nil {
			log.WithError(err).Error("Error when trying to close netns file.")
		}
	}
	return nil
}

type NetNS struct {
	file *os.File
}

func (n *NetNS) Handle() netns.NsHandle {
	fd := n.file.Fd()
	return netns.NsHandle(fd)
}

func (n *NetNS) Close() error {
	return n.file.Close()
}

// ListNetNSIDs scans the system for network namespaces and returns a cache indexed on NSID.
// NSIDs are the IDs of "peer" namespaces.  They are local to each namespace and they are
// only assigned to namespaces that have a link to the current one (for example a veth between
// the namespaces).
//
// The cache holds open file handles for each namespace so called must call Close() once they're
// done with it.
func ListNetNSIDs() (*CachedNetNSList, error) {
	// Build up a list of candidate netns files by scanning all the processes in the system
	// and the pinning directory used by "ip netns":
	var netnsPaths []string
	numbersRegex := regexp.MustCompile(`\d+`)
	proc, err := ioutil.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("failed to read /proc: %w", err)
	}
	for _, f := range proc {
		if f.IsDir() && numbersRegex.MatchString(f.Name()) {
			netnsPath := fmt.Sprintf("/proc/%s/ns/net", f.Name())
			netnsPaths = append(netnsPaths, netnsPath)
		}
	}
	pinnedNetnsDir, err := ioutil.ReadDir("/var/run/netns")
	if err != nil {
		if os.IsNotExist(err) {
			log.Debug("/var/run/netns doesn't exist, will only search PIDs.")
		} else {
			return nil, fmt.Errorf("failed to read /var/run/netns: %w", err)
		}
	} else {
		for _, f := range pinnedNetnsDir {
			netnsPath := fmt.Sprintf("/var/run/netns/%s", f.Name())
			netnsPaths = append(netnsPaths, netnsPath)
		}
	}

	// Scan the netns files, open one file per inode (i.e. one per netns) and record the mapping from
	// netns ID (local to our namespace) to netns information.
	inodeIDDone := map[uint64]bool{}
	idToNetnsInfo := map[int]NetNS{}
	for _, netnsPath := range netnsPaths {
		// Get the inode number (or detect if the file has gone away).
		netnsStat, err := os.Stat(netnsPath)
		if err != nil {
			if os.IsNotExist(err) {
				// File probably gone; this is very likely for /proc/ files which disappear
				// with their process.
				log.WithField("path", netnsPath).Debug("Netns file no longer present")
			} else {
				log.WithField("path", netnsPath).WithError(err).Warn("Unexpected error querying netns file.")
			}
			continue
		}
		unixInfo := netnsStat.Sys().(*syscall.Stat_t)
		if inodeIDDone[unixInfo.Ino] {
			continue // Already done this netns.
		}

		// Found a new inode. Try to open the file and store it.
		netnsFile, err := os.Open(netnsPath)
		if err != nil {
			if os.IsNotExist(err) {
				// File probably gone; this is very likely for /proc/ files which disappear
				// with their process.
				log.WithField("path", netnsPath).Debug("Netns file no longer present")
			} else {
				log.WithField("path", netnsPath).WithError(err).Warn("Unexpected error querying netns file.")
			}
			continue
		}
		id, err := netlink.GetNetNsIdByFd(int(netnsFile.Fd()))
		if err != nil {
			log.WithField("path", netnsPath).WithError(err).Warn("Unexpected error getting netns ID.")
			_ = netnsFile.Close()
			continue
		}
		inodeIDDone[unixInfo.Ino] = true
		if id >= 0 {
			idToNetnsInfo[id] = NetNS{
				file: netnsFile,
			}
		}
	}
	return &CachedNetNSList{
		idToNetNS: idToNetnsInfo,
	}, nil
}
