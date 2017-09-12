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

package main

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/containernetworking/cni/pkg/ns"
	"github.com/docopt/docopt-go"
	"github.com/projectcalico/felix/fv/utils"
	log "github.com/sirupsen/logrus"
)

const usage = `test-connection: test connection to some target, for Felix FV testing.

Usage:
  test-connection <namespace-path> <ip-address> <port>

If connection is successful, test-connection exits successfully.

If connection is unsuccessful, test-connection panics and so exits with a failure status.`

func main() {
	log.SetLevel(log.DebugLevel)

	arguments, err := docopt.Parse(usage, nil, true, "v0.1", false)
	if err != nil {
		println(usage)
		log.WithError(err).Fatal("Failed to parse usage")
	}
	namespacePath := arguments["<namespace-path>"].(string)
	ipAddress := arguments["<ip-address>"].(string)
	port := arguments["<port>"].(string)

	// Get the specified network namespace (representing a workload).
	namespace, err := ns.GetNS(namespacePath)
	if err != nil {
		panic(err)
	}
	log.WithField("namespace", namespace).Debug("Got namespace")

	// Now, in that namespace, try connecting to the target.
	err = namespace.Do(func(_ ns.NetNS) error {

		err := utils.RunCommand("ip", "r")
		if err != nil {
			return err
		}

		const testMessage = "hello"

		conn, err := net.DialTimeout("tcp", ipAddress+":"+port, 1*time.Second)
		if err != nil {
			return err
		}
		defer conn.Close()

		fmt.Fprintf(conn, testMessage+"\n")
		reply, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			return err
		}
		reply = strings.TrimSpace(reply)
		if reply != testMessage {
			return errors.New("Unexpected reply: " + reply)
		}

		return nil
	})

	if err != nil {
		panic(err)
	}
}
