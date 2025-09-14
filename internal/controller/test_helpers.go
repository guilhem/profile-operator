/*
Copyright 2025.

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

package controller

import (
	"encoding/json"

	"k8s.io/apimachinery/pkg/runtime"
)

// createSimpleOverlay creates a simple overlay for testing
func createSimpleOverlay(labels map[string]string, spec map[string]interface{}) *runtime.RawExtension {
	overlay := make(map[string]interface{})

	if len(labels) > 0 {
		metadata := map[string]interface{}{
			"labels": labels,
		}
		overlay["metadata"] = metadata
	}

	if len(spec) > 0 {
		overlay["spec"] = spec
	}

	overlayJSON, _ := json.Marshal(overlay)
	return &runtime.RawExtension{Raw: overlayJSON}
}

// createMetadataOnlyOverlay creates an overlay with only metadata
func createMetadataOnlyOverlay(labels map[string]string) *runtime.RawExtension {
	return createSimpleOverlay(labels, nil)
}
