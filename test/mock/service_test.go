// Copyright 2017 Istio Authors
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

package mock

import "testing"

func TestMockServices(t *testing.T) {
	for _, svc := range Discovery.Services() {
		if err := svc.Validate(); err != nil {
			t.Errorf("%v.Validate() => Got %v", svc, err)
		}
		instances := Discovery.Instances(svc.Hostname, svc.Ports.GetNames(), nil)
		if len(instances) == 0 {
			t.Errorf("Discovery.Instances => Got %d, want positive", len(instances))
		}
		for _, instance := range instances {
			if err := instance.Validate(); err != nil {
				t.Errorf("%v.Validate() => Got %v", instance, err)
			}
		}
	}
}
