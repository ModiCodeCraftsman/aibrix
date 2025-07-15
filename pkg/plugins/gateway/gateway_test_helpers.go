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

package gateway

import (
	"github.com/stretchr/testify/mock"
	"github.com/vllm-project/aibrix/pkg/metrics"
	"github.com/vllm-project/aibrix/pkg/types"
	"github.com/vllm-project/aibrix/pkg/utils"
	v1 "k8s.io/api/core/v1"
)

// mockRouter implements types.Router interface for testing
type mockRouter struct {
	mock.Mock
}

func (m *mockRouter) Route(ctx *types.RoutingContext, pods types.PodList) (string, error) {
	args := m.Called(ctx, pods)
	return args.String(0), args.Error(1)
}
func (m *mockRouter) Name() string { return "mock-router" }

// MockCache implements cache.Cache interface for testing
type MockCache struct {
	mock.Mock
}

func (m *MockCache) HasModelKey(modelKey utils.ModelKey) bool {
	args := m.Called(modelKey)
	return args.Bool(0)
}

func (m *MockCache) ListPodsByModelKey(modelKey utils.ModelKey) (types.PodList, error) {
	args := m.Called(modelKey)
	return args.Get(0).(types.PodList), args.Error(1)
}

func (m *MockCache) AddRequestCountByModelKey(ctx *types.RoutingContext, requestID string, modelKey utils.ModelKey) int64 {
	args := m.Called(ctx, requestID, modelKey)
	return args.Get(0).(int64)
}

func (m *MockCache) DoneRequestCountByModelKey(ctx *types.RoutingContext, requestID string, modelKey utils.ModelKey, term int64) {
	m.Called(ctx, requestID, modelKey, term)
}

func (m *MockCache) DoneRequestTraceByModelKey(ctx *types.RoutingContext, requestID string, modelKey utils.ModelKey, inputTokens, outputTokens, term int64) {
	m.Called(ctx, requestID, modelKey, inputTokens, outputTokens, term)
}

func (m *MockCache) AddSubscriber(subscriber metrics.MetricSubscriber) {
	m.Called(subscriber)
}

func (m *MockCache) GetMetricValueByPodKey(podKey utils.PodKey, metricName string) (metrics.MetricValue, error) {
	args := m.Called(podKey, metricName)
	return args.Get(0).(metrics.MetricValue), args.Error(1)
}

func (m *MockCache) GetMetricValueByPodModelKey(podKey utils.PodKey, modelKey utils.ModelKey, metricName string) (metrics.MetricValue, error) {
	args := m.Called(podKey, modelKey, metricName)
	return args.Get(0).(metrics.MetricValue), args.Error(1)
}

func (m *MockCache) GetPodByKey(key utils.PodKey) (*v1.Pod, error) {
	args := m.Called(key)
	return args.Get(0).(*v1.Pod), args.Error(1)
}

func (m *MockCache) ListModels(tenantID string) []string {
	args := m.Called(tenantID)
	return args.Get(0).([]string)
}

func (m *MockCache) ListModelsByPodKey(podKey utils.PodKey) ([]string, error) {
	args := m.Called(podKey)
	return args.Get(0).([]string), args.Error(1)
}
