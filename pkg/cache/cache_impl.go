/*
Copyright 2024 The Aibrix Team.

Licensed under the Apache License, Version 2.0 (the "License");
you m// ListModelsByPod lists models associated with a Pod
// Parameters:
//
//	podName: Name of the Pod to query
//	podNamespace: Namespace of the Pod to query
//	tenantID: ID of the tenant (defaults to "default" if empty)
//
// Returns:
//
//	[]string: Slice of model names
//	error: Error if Pod doesn't exist
func (c *Store) ListModelsByPod(podName, podNamespace, tenantID string) ([]string, error) {
	podKey := utils.NewPodKey(podNamespace, podName, tenantID)
	return c.ListModelsByPodKey(podKey)
}e except in compliance with the License.
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
	"fmt"
	"sync/atomic"

	"github.com/vllm-project/aibrix/pkg/constants"
	"github.com/vllm-project/aibrix/pkg/metrics"
	"github.com/vllm-project/aibrix/pkg/types"
	"github.com/vllm-project/aibrix/pkg/utils"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

// GetPodByKey retrieves a Pod object by PodKey from the cache
// Parameters:
//
//	key: PodKey containing namespace, name, and tenant information
//
// Returns:
//
//	*v1.Pod: The found Pod object
//	error: Error if pod doesn't exist
func (c *Store) GetPodByKey(key utils.PodKey) (*v1.Pod, error) {
	// Only look for tenant-aware key format
	metaPod, ok := c.metaPods.Load(key.String())
	if !ok {
		return nil, fmt.Errorf("key does not exist in the cache: %s", key.String())
	}

	return metaPod.Pod, nil
}

// ListPods returns all cached Pod objects
// Do not call this directly, for debug purpose and less efficient.
// Returns:
//
//	[]*v1.Pod: Slice of Pod objects
func (c *Store) ListPods() []*v1.Pod {
	// Use a map to deduplicate pods (since they might be stored with multiple keys)
	uniquePods := make(map[string]*v1.Pod)

	c.metaPods.Range(func(_ string, metaPod *Pod) bool {
		// Use pod namespace/name as a unique identifier
		key := fmt.Sprintf("%s/%s", metaPod.Pod.Namespace, metaPod.Pod.Name)
		uniquePods[key] = metaPod.Pod
		return true
	})

	// Convert the map to a slice
	pods := make([]*v1.Pod, 0, len(uniquePods))
	for _, pod := range uniquePods {
		pods = append(pods, pod)
	}
	return pods
}

// ListPodsByModelKey gets Pods associated with a specific model
// Parameters:
//
//	modelKey: ModelKey containing model name and tenant information
//
// Returns:
//
//	types.PodList: List of Pod objects
//	error: Error if model doesn't exist
func (c *Store) ListPodsByModelKey(modelKey utils.ModelKey) (types.PodList, error) {
	// Only use the tenant-aware model key format
	modelKeyStr := modelKey.String()
	meta, ok := c.metaModels.Load(modelKeyStr)
	if !ok {
		return nil, fmt.Errorf("model does not exist in the cache: %s (tenant: %s)", modelKey.Name, modelKey.TenantID)
	}

	return meta.Pods.Array(), nil
}

// ListModels returns all cached model names
// Parameters:
//
//	tenantID: ID of the tenant (defaults to "default" if empty)
//
// Returns:
//
//	[]string: Slice of model names
func (c *Store) ListModels(tenantID string) []string {
	if tenantID == "" {
		tenantID = constants.DefaultTenantID
	}

	// Get all model keys
	allKeys := c.metaModels.Keys()

	// Filter keys by tenant
	modelNames := make([]string, 0)

	for _, keyString := range allKeys {
		modelKey, success := utils.ParseModelKeyString(keyString)
		if success && modelKey.TenantID == tenantID {
			modelNames = append(modelNames, modelKey.Name)
		}
	}

	return modelNames
}

// HasModelKey checks if a model exists in the cache
// Parameters:
//
//	modelKey: ModelKey containing model name and tenant information
//
// Returns:
//
//	bool: True if model exists
func (c *Store) HasModelKey(modelKey utils.ModelKey) bool {
	_, ok := c.metaModels.Load(modelKey.String())
	return ok
}

// GetMetricValueByPodKey retrieves metric value for a Pod
// Parameters:
//
//	podKey: PodKey containing namespace, name, and tenant information
//	metricName: Name of the metric
//
// Returns:
//
//	metrics.MetricValue: The metric value
//	error: Error if Pod or metric doesn't exist
func (c *Store) GetMetricValueByPodKey(podKey utils.PodKey, metricName string) (metrics.MetricValue, error) {
	metaPod, ok := c.metaPods.Load(podKey.String())
	if !ok {
		return nil, fmt.Errorf("key does not exist in the cache: %s", podKey.String())
	}

	return c.getPodMetricImpl(podKey.Name, &metaPod.Metrics, metricName)
}

// GetMetricValueByPodModelKey retrieves metric value for Pod-Model combination
// Parameters:
//
//	podKey: PodKey containing namespace, name, and tenant information
//	modelKey: ModelKey containing model name and tenant information
//	metricName: Name of the metric
//
// Returns:
//
//	metrics.MetricValue: The metric value
//	error: Error if Pod, model or metric doesn't exist
func (c *Store) GetMetricValueByPodModelKey(podKey utils.PodKey, modelKey utils.ModelKey, metricName string) (metrics.MetricValue, error) {
	metaPod, ok := c.metaPods.Load(podKey.String())
	if !ok {
		return nil, fmt.Errorf("key does not exist in the cache: %s", podKey.String())
	}

	return c.getPodMetricImpl(podKey.Name, &metaPod.ModelMetrics, c.getPodModelMetricName(modelKey.Name, metricName))
}

// AddRequestCountByModelKey tracks new request initiation
// Parameters:
//
//	ctx: Routing context
//	requestID: Unique request identifier
//	modelKey: ModelKey containing model name and tenant information
//
// Returns:
//
//	int64: Trace term identifier
func (c *Store) AddRequestCountByModelKey(ctx *types.RoutingContext, requestID string, modelKey utils.ModelKey) (traceTerm int64) {
	if enableGPUOptimizerTracing {
		success := false
		for {
			trace := c.getRequestTrace(modelKey.String())
			// TODO: use non-empty key if we have output prediction to decide buckets early.
			if traceTerm, success = trace.AddRequest(requestID, ""); success {
				break
			}
			// In case AddRequest return false, it has been recycled and we want to retry.
		}
	}

	meta, ok := c.metaModels.Load(modelKey.String())
	if ok {
		atomic.AddInt32(&meta.pendingRequests, 1)
	}

	if ctx != nil {
		c.addPodStats(ctx, requestID)
	}
	return
}

// DoneRequestCountByModelKey completes request tracking
// Parameters:
//
//	ctx: Routing context
//	requestID: Unique request identifier
//	modelKey: ModelKey containing model name and tenant information
//	traceTerm: Trace term identifier
func (c *Store) DoneRequestCountByModelKey(ctx *types.RoutingContext, requestID string, modelKey utils.ModelKey, traceTerm int64) {
	if ctx != nil {
		c.donePodStats(ctx, requestID)
	}

	meta, ok := c.metaModels.Load(modelKey.String())
	if ok {
		atomic.AddInt32(&meta.pendingRequests, -1)
	}

	// DoneRequest only works for current term, no need to retry.
	if enableGPUOptimizerTracing {
		c.getRequestTrace(modelKey.String()).DoneRequest(requestID, traceTerm)
	}
}

// DoneRequestTraceByModelKey completes request tracing
// Parameters:
//
//	ctx: Routing context
//	requestID: Unique request identifier
//	modelKey: ModelKey containing model name and tenant information
//	inputTokens: Input tokens count
//	outputTokens: Output tokens count
//	traceTerm: Trace term identifier
func (c *Store) DoneRequestTraceByModelKey(ctx *types.RoutingContext, requestID string, modelKey utils.ModelKey, inputTokens, outputTokens, traceTerm int64) {
	if ctx != nil {
		c.donePodStats(ctx, requestID)
	}

	meta, ok := c.metaModels.Load(modelKey.String())
	if ok {
		atomic.AddInt32(&meta.pendingRequests, -1)
	}

	if enableGPUOptimizerTracing {
		var traceKey string
		for {
			trace := c.getRequestTrace(modelKey.String())
			if traceKey, ok = trace.DoneRequestTrace(requestID, inputTokens, outputTokens, traceKey, traceTerm); ok {
				break
			}
			// In case DoneRequest return false, it has been recycled and we want to retry.
		}
		klog.V(5).Infof("inputTokens: %v, outputTokens: %v, trace key: %s", inputTokens, outputTokens, traceKey)
	}
}

// AddSubscriber registers new metric subscriber
// Parameters:
//
//	subscriber: Metric subscriber implementation
func (c *Store) AddSubscriber(subscriber metrics.MetricSubscriber) {
	c.subscribers = append(c.subscribers, subscriber)
	c.aggregateMetrics()
}

// ListModelsByPodKey lists models associated with a Pod
// Parameters:
//
//	podKey: PodKey containing namespace, name, and tenant information
//
// Returns:
//
//	[]string: Slice of model names
//	error: Error if Pod doesn't exist
func (c *Store) ListModelsByPodKey(podKey utils.PodKey) ([]string, error) {
	// Only look for tenant-aware key format
	metaPod, ok := c.metaPods.Load(podKey.String())
	if !ok {
		return nil, fmt.Errorf("key does not exist in the cache: %s", podKey.String())
	}

	return metaPod.Models.Array(), nil
}
