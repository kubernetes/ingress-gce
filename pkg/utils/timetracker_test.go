/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utils

import (
	"testing"
	"time"
)

func TestTimeTracker(t *testing.T) {
	tt := NewTimeTracker()
	trials := 3
	for i := 0; i < trials; i++ {
		timestamp := tt.Track()
		result := tt.Get()
		if timestamp != result {
			t.Errorf("In trial %d, expect %v == %v", i, timestamp, result)
		}
		now := time.Now()
		tt.Set(now)
		result = tt.Get()
		if now != result {
			t.Errorf("In trial %d, expect %v == %v", i, now, result)
		}
	}

}
