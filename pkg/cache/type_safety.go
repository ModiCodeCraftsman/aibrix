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

package cache

import (
	"github.com/vllm-project/aibrix/pkg/utils"
)

// ==================== Cache interface extension for type safety ====================

// WithTypeKeyAccess is an extension of the Cache interface that provides access
// to the underlying maps using struct keys directly for type safety.
type WithTypeKeyAccess interface {
	Cache
	// Internal maps access using struct keys
	GetPodMap() *utils.SyncMap[utils.PodKey, *Pod]
	GetModelMap() *utils.SyncMap[utils.ModelKey, *Model]
}

// GetPodMap returns the underlying Pod map that uses struct keys
func (c *Store) GetPodMap() *utils.SyncMap[utils.PodKey, *Pod] {
	return &c.metaPods
}

// GetModelMap returns the underlying Model map that uses struct keys
func (c *Store) GetModelMap() *utils.SyncMap[utils.ModelKey, *Model] {
	return &c.metaModels
}

// ==================== Type conversion helpers ====================

// PodKeyFromString converts a string key to a PodKey struct
func PodKeyFromString(key string) (utils.PodKey, bool) {
	return utils.ParsePodKeyString(key)
}

// ModelKeyFromString converts a string key to a ModelKey struct
func ModelKeyFromString(key string) (utils.ModelKey, bool) {
	return utils.ParseModelKeyString(key)
}
