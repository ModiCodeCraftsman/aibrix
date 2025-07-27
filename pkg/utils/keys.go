/*
Copyright 2025 The Aibrix Team.

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
	"fmt"
	"strings"

	"github.com/vllm-project/aibrix/pkg/constants"
	"k8s.io/klog/v2"
)

// PodKey represents a unique identifier for a pod with tenant information
type PodKey struct {
	Namespace string
	Name      string
	TenantID  string
}

// NewPodKey creates a new PodKey with the given namespace, name, and tenant ID
// If tenantID is empty, it defaults to "default"
func NewPodKey(namespace, name, tenantID string) PodKey {
	if tenantID == "" {
		tenantID = constants.DefaultTenantID
	}
	return PodKey{
		Namespace: namespace,
		Name:      name,
		TenantID:  tenantID,
	}
}

// ParsePodKeyString parses a string in the format "tenant/namespace/podName" or legacy format "namespace/podName"
// into a PodKey struct. For legacy format, tenant is set to "default".
func ParsePodKeyString(key string) (PodKey, bool) {
	parts := strings.Split(key, "/")
	if len(parts) == 3 {
		// New format: tenant/namespace/podName
		return PodKey{
			TenantID:  parts[0],
			Namespace: parts[1],
			Name:      parts[2],
		}, true
	} else if len(parts) == 2 {
		// Legacy format: namespace/podName
		return PodKey{
			TenantID:  constants.DefaultTenantID,
			Namespace: parts[0],
			Name:      parts[1],
		}, true
	}
	klog.V(4).Infof("Invalid key format: %q. Expected format: tenant/namespace/name or namespace/name", key)
	return PodKey{}, false
}

// String returns the string representation of the PodKey in the format "tenant/namespace/name"
func (k PodKey) String() string {
	return fmt.Sprintf("%s/%s/%s", k.TenantID, k.Namespace, k.Name)
}

// ModelKey represents a unique identifier for a model with tenant information
type ModelKey struct {
	Name     string
	TenantID string
}

// NewModelKey creates a new ModelKey with the given name and tenant ID
// If tenantID is empty, it defaults to "default"
func NewModelKey(name, tenantID string) ModelKey {
	if tenantID == "" {
		tenantID = constants.DefaultTenantID
	}
	return ModelKey{
		Name:     name,
		TenantID: tenantID,
	}
}

// ParseModelKeyString parses a string in the format "tenant/modelName" or legacy format "modelName"
// into a ModelKey struct. For legacy format, tenant is set to "default".
func ParseModelKeyString(key string) (ModelKey, bool) {
	parts := strings.Split(key, "/")
	if len(parts) == 2 {
		// New format: tenant/modelName
		return ModelKey{
			TenantID: parts[0],
			Name:     parts[1],
		}, true
	} else if len(parts) == 1 {
		// Legacy format: modelName
		return ModelKey{
			TenantID: constants.DefaultTenantID,
			Name:     parts[0],
		}, true
	}
	klog.V(4).Infof("Invalid model key format: %q. Expected format: tenant/modelName or modelName", key)
	return ModelKey{}, false
}

// String returns the string representation of the ModelKey in the format "tenant/name"
func (k ModelKey) String() string {
	return fmt.Sprintf("%s/%s", k.TenantID, k.Name)
}
