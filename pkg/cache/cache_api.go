/*
Copyright 2024 The Aibrix Team.

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
	"github.com/vllm-project/aibrix/pkg/metrics"
	"github.com/vllm-project/aibrix/pkg/types"
	"github.com/vllm-project/aibrix/pkg/utils"
	v1 "k8s.io/api/core/v1"
)

// Cache is the root interface aggregating caching functionalities
type Cache interface {
	PodCache
	ModelCache
	MetricCache
	RequestTracker
}

// PodCache defines operations for pod information caching
type PodCache interface {
	// GetPodByKey retrieves a Pod object by PodKey
	// Parameters:
	//   key: PodKey containing namespace, name, and tenant information
	// Returns:
	//   *v1.Pod: Found pod object
	//   error: Error information if operation fails
	GetPodByKey(key utils.PodKey) (*v1.Pod, error)

	// ListPodsByModelKey gets pods associated with a model
	// Parameters:
	//   modelKey: ModelKey containing model name and tenant information
	// Returns:
	//   types.PodList: Pod objects matching the criteria
	//   error: Error information if operation fails
	ListPodsByModelKey(modelKey utils.ModelKey) (types.PodList, error)
}

// ModelCache defines operations for model information caching
type ModelCache interface {
	// HasModelKey checks existence of a model by ModelKey
	// Parameters:
	//   modelKey: ModelKey containing model name and tenant information
	// Returns:
	//   bool: True if model exists, false otherwise
	HasModelKey(modelKey utils.ModelKey) bool

	// ListModels gets all model names for a tenant
	// Parameters:
	//   tenantID: ID of the tenant (defaults to "default" if empty)
	// Returns:
	//   []string: List of model names
	ListModels(tenantID string) []string

	// ListModelsByPodKey gets models associated with a pod
	// Parameters:
	//   podKey: PodKey containing namespace, name, and tenant information
	// Returns:
	//   []string: List of model names
	//   error: Error information if operation fails
	ListModelsByPodKey(podKey utils.PodKey) ([]string, error)
}

// MetricCache defines operations for metric data caching
type MetricCache interface {
	// GetMetricValueByPodKey gets metric value for a pod
	// Parameters:
	//   podKey: PodKey containing namespace, name, and tenant information
	//   metricName: Name of the metric
	// Returns:
	//   metrics.MetricValue: Retrieved metric value
	//   error: Error information if operation fails
	GetMetricValueByPodKey(podKey utils.PodKey, metricName string) (metrics.MetricValue, error)

	// GetMetricValueByPodModelKey gets metric value for pod-model pair
	// Parameters:
	//   podKey: PodKey containing namespace, name, and tenant information
	//   modelKey: ModelKey containing model name and tenant information
	//   metricName: Name of the metric
	// Returns:
	//   metrics.MetricValue: Retrieved metric value
	//   error: Error information if operation fails
	GetMetricValueByPodModelKey(podKey utils.PodKey, modelKey utils.ModelKey, metricName string) (metrics.MetricValue, error)

	// AddSubscriber registers a new metrics subscriber
	// Parameters:
	//   subscriber: The metrics subscriber implementation
	AddSubscriber(subscriber metrics.MetricSubscriber)
}

// RequestTracker defines operations for track workload statistics
type RequestTracker interface {
	// AddRequestCountByModelKey starts tracking request count
	// Parameters:
	//   ctx: Routing context
	//   requestID: Unique request identifier
	//   modelKey: ModelKey containing model name and tenant information
	// Returns:
	//   int64: Trace term identifier
	AddRequestCountByModelKey(ctx *types.RoutingContext, requestID string, modelKey utils.ModelKey) (traceTerm int64)

	// DoneRequestCountByModelKey completes request count tracking, only one DoneRequestXXX should be called for a request
	// Parameters:
	//   ctx: Routing context
	//   requestID: Unique request identifier
	//   modelKey: ModelKey containing model name and tenant information
	//   traceTerm: Trace term identifier
	DoneRequestCountByModelKey(ctx *types.RoutingContext, requestID string, modelKey utils.ModelKey, traceTerm int64)

	// DoneRequestTraceByModelKey completes request tracing, only one DoneRequestXXX should be called for a request
	// Parameters:
	//   ctx: Routing context
	//   requestID: Unique request identifier
	//   modelKey: ModelKey containing model name and tenant information
	//   inputTokens: Number of input tokens
	//   outputTokens: Number of output tokens
	//   traceTerm: Trace term identifier
	DoneRequestTraceByModelKey(ctx *types.RoutingContext, requestID string, modelKey utils.ModelKey, inputTokens, outputTokens, traceTerm int64)
}
