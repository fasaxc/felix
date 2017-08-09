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

package calc_test

import (
	log "github.com/sirupsen/logrus"

	"github.com/projectcalico/libcalico-go/lib/selector"
)

func selectorId(selStr string) string {
	sel, err := selector.Parse(selStr)
	if err != nil {
		log.Fatalf("Failed to parse %v: %v", selStr, err)
	}
	return sel.UniqueID()
}
