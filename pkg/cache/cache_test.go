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
	"fmt"
	"math"
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	modelv1alpha1 "github.com/vllm-project/aibrix/api/model/v1alpha1"
	"github.com/vllm-project/aibrix/pkg/constants"
	"github.com/vllm-project/aibrix/pkg/metrics"
	"github.com/vllm-project/aibrix/pkg/utils"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

// For test helpers
var _ = &v1.Pod{
	ObjectMeta: metav1.ObjectMeta{
		Name: "testpod",
	},
}

func getReadyPod(podName, podNamespcae string, modelName string, id int) *v1.Pod {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: podNamespcae,
			Labels:    make(map[string]string),
		},
		Status: v1.PodStatus{
			PodIP: fmt.Sprintf("10.0.0.%d", id),
			Conditions: []v1.PodCondition{
				{
					Type:   v1.PodReady,
					Status: v1.ConditionTrue,
				},
			},
		},
	}
	pod.ObjectMeta.Labels[modelIdentifier] = modelName
	return pod
}

func getNewPod(podName, podNamespace string, modelName string, id int) *v1.Pod {
	pod := getReadyPod(podName, podNamespace, modelName, id)
	pod.Status.Conditions[0].Type = v1.PodInitialized
	return pod
}

func getNewModelAdapter(modelName, namespace string, podName string) *modelv1alpha1.ModelAdapter {
	adapter := &modelv1alpha1.ModelAdapter{
		ObjectMeta: metav1.ObjectMeta{
			Name:      modelName,
			Namespace: namespace,
		},
		Status: modelv1alpha1.ModelAdapterStatus{
			Instances: []string{podName},
		},
	}
	if podName == "" {
		adapter.Status.Instances = nil
	}
	return adapter
}

func getNewModelAdapterWithPods(modelName, namespace string, podNames []string) *modelv1alpha1.ModelAdapter {
	adapter := &modelv1alpha1.ModelAdapter{
		ObjectMeta: metav1.ObjectMeta{
			Name:      modelName,
			Namespace: namespace,
		},
		Status: modelv1alpha1.ModelAdapterStatus{
			Instances: podNames,
		},
	}
	return adapter
}

func newCache() *Store {
	return &Store{
		initialized: true,
	}
}

func newTraceCache() *Store {
	enableGPUOptimizerTracing = true
	return &Store{
		initialized:  true,
		requestTrace: &utils.SyncMap[string, *RequestTrace]{},
	}
}

func TestCache(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Cache Suite")
}

func (c *Store) AddPod(obj interface{}) {
	c.addPod(obj)
}

type lagacyCache struct {
	requestTrace map[string]map[string]int
	mu           sync.RWMutex
}

func (c *lagacyCache) AddRequestTrace(modelName string, inputTokens, outputTokens int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	inputIndex := int64(math.Round(math.Log2(float64(inputTokens)) / RequestTracePrecision)) // Round to the nearest precision and convert to int
	outputIndex := int64(math.Round(math.Log2(float64(outputTokens)) / RequestTracePrecision))

	klog.V(5).Infof("inputTokens: %v, inputIndex: %v, outputTokens: %v, outputIndex: %v",
		inputTokens, inputIndex, outputTokens, outputIndex)

	if len(c.requestTrace[modelName]) == 0 {
		c.requestTrace[modelName] = map[string]int{}
	}

	c.requestTrace[modelName][fmt.Sprintf("%v:%v", inputIndex, outputIndex)] += 1
}

var _ = Describe("Cache", func() {
	It("should both addPod create both pods and metaModels entry", func() {
		cache := newCache()

		// Ignore pods without model label
		podWOModel := getReadyPod("p1", "default", "m1", 0)
		podWOModel.ObjectMeta.Labels = nil
		cache.addPod(podWOModel)

		// Using tenant-aware key format for pod lookups
		tenantPodKey := utils.GeneratePodKey("default", "p1", constants.DefaultTenantID)
		_, exist := cache.metaPods.Load(tenantPodKey)
		Expect(exist).To(BeFalse())

		// Ignore pods without model label
		podRayWorker := getReadyPod("p1", "default", "m1", 0)
		podRayWorker.ObjectMeta.Labels[nodeType] = nodeWorker
		cache.addPod(podRayWorker)

		// Using tenant-aware key format for pod lookups
		_, exist = cache.metaPods.Load(tenantPodKey)
		Expect(exist).To(BeFalse())

		pod := getReadyPod("p1", "default", "m1", 0)
		cache.addPod(pod)

		// Print all keys in metaModels for debugging
		fmt.Println("Keys in metaModels:")
		cache.metaModels.Range(func(key string, model *Model) bool {
			fmt.Printf("  %s\n", key)

			// Inspect pods in this model using the Array() method
			fmt.Printf("  Pods in model %s: %d pods\n", key, model.Pods.Len())
			if podArray := model.Pods.Array(); podArray != nil {
				for i, pod := range podArray.Pods {
					fmt.Printf("    Pod %d: %s/%s\n", i, pod.Namespace, pod.Name)
				}
			}

			return true
		})

		// Pod meta exists - using tenant-aware key format
		tenantPodKey = utils.GeneratePodKey("default", "p1", constants.DefaultTenantID)
		metaPod, exist := cache.metaPods.Load(tenantPodKey)
		Expect(exist).To(BeTrue())
		Expect(metaPod.Pod).To(Equal(pod))
		Expect(metaPod.Models).ToNot(BeNil())

		// Pod > model mapping exists.
		modelName, exist := metaPod.Models.Load("m1")
		Expect(exist).To(BeTrue())
		Expect(modelName).To(Equal("m1"))

		// Model meta exists - using tenant-aware key format
		modelKey := utils.GenerateModelKey("m1", constants.DefaultTenantID)
		metaModel, exist := cache.metaModels.Load(modelKey)
		Expect(exist).To(BeTrue())
		Expect(metaModel).ToNot(BeNil())
		Expect(metaModel.Pods).ToNot(BeNil())

		// Model -> pod mapping exists - check through array since we can't directly access by key
		podArray := metaModel.Pods.Array()
		Expect(podArray).ToNot(BeNil())
		Expect(podArray.Len()).To(Equal(1))
		Expect(podArray.Pods[0]).To(Equal(pod))
		// We'll not test podArray functionality here.
	})

	It("should addModelAdapter create metaModels entry", func() {
		cache := newCache()
		cache.addPod(getReadyPod("p1", "default", "m1", 0))

		// Success
		cache.addModelAdapter(getNewModelAdapter("m1adapter", "default", "p1")) // Print all keys in metaModels for debugging
		fmt.Println("Keys in metaModels after adding model adapter:")
		cache.metaModels.Range(func(key string, model *Model) bool {
			fmt.Printf("  %s\n", key)

			// Inspect pods in this model using the Array() method
			fmt.Printf("  Pods in model %s: %d pods\n", key, model.Pods.Len())
			if podArray := model.Pods.Array(); podArray != nil {
				for i, pod := range podArray.Pods {
					fmt.Printf("    Pod %d: %s/%s\n", i, pod.Namespace, pod.Name)
				}
			}

			return true
		})

		// Using tenant-aware key format for pod lookups
		tenantPodKey := utils.GeneratePodKey("default", "p1", constants.DefaultTenantID)
		metaPod, exist := cache.metaPods.Load(tenantPodKey)
		Expect(exist).To(BeTrue())
		Expect(metaPod.Models.Len()).To(Equal(2))
		// Pod -> adapter mapping exists
		modelName, exist := metaPod.Models.Load("m1adapter")
		Expect(exist).To(BeTrue())
		Expect(modelName).To(Equal("m1adapter"))
		// Model adapter meta exists - using the tenant-aware format
		modelKey := utils.GenerateModelKey("m1adapter", constants.DefaultTenantID)
		metaModel, exist := cache.metaModels.Load(modelKey)
		Expect(exist).To(BeTrue())
		Expect(metaModel).ToNot(BeNil())
		Expect(metaModel.Pods).ToNot(BeNil())
		// Model adapter -> pod mapping exists
		podKey := utils.GeneratePodKey("default", "p1", constants.DefaultTenantID)
		modelPod, exist := metaModel.Pods.Load(podKey)

		// Add debug output to help diagnose
		if !exist {
			fmt.Printf("Failed to find pod with key: %s\n", podKey)

			// Let's print the pods from the model array for reference
			fmt.Println("Pods in this model:")
			if podArray := metaModel.Pods.Array(); podArray != nil {
				for i, pod := range podArray.Pods {
					fmt.Printf("  Pod %d: %s/%s\n", i, pod.Namespace, pod.Name)
				}
			}
		}

		Expect(exist).To(BeTrue())
		Expect(modelPod).To(Equal(metaPod.Pod))
		pods := metaModel.Pods.Array()
		Expect(pods.Len()).To(Equal(1))
		Expect(pods.Pods[0]).To(Equal(metaPod.Pod))

		// Failure
		cache.addModelAdapter(getNewModelAdapter("p0", "default", "m0adapter"))
		// No pod meta automatically created - using tenant-aware key format
		failureTenantPodKey := utils.GeneratePodKey("default", "p0", constants.DefaultTenantID)
		_, exist = cache.metaPods.Load(failureTenantPodKey)
		Expect(exist).To(BeFalse())
		// No model meta created on failure
		failureModelKey := utils.GenerateModelKey("m0adapter", constants.DefaultTenantID)
		_, exist = cache.metaModels.Load(failureModelKey)
		Expect(exist).To(BeFalse())
	})

	It("should updatePod clear old mappings with no model adapter inherited", func() {
		cache := newCache()
		oldPod := getReadyPod("p1", "default", "m1", 0)
		cache.addPod(oldPod)
		cache.addModelAdapter(getNewModelAdapter("m1adapter", "default", oldPod.Name))

		// Check if the pod was actually added to the cache - using tenant-aware key format
		tenantPodKey := utils.GeneratePodKey(oldPod.Namespace, oldPod.Name, constants.DefaultTenantID)
		oldMetaPod, ok := cache.metaPods.Load(tenantPodKey)
		Expect(ok).To(BeTrue(), "Pod should be in the cache")

		// Only try to update metrics if the pod exists
		if ok {
			err := cache.updatePodRecord(oldMetaPod, "", metrics.NumRequestsRunning, metrics.PodMetricScope, &metrics.LabelValueMetricValue{Value: "0"})
			Expect(err).To(BeNil())
			Expect(oldMetaPod.Models.Len()).To(Equal(2))
			Expect(oldMetaPod.Metrics.Len()).To(Equal(1))
		}

		newPod := getReadyPod("p2", "default", "m1", 1) // IP may changed due to migration

		// Print all keys in metaModels before updatePod
		fmt.Println("Keys in metaModels before updatePod:")
		cache.metaModels.Range(func(key string, model *Model) bool {
			fmt.Printf("  %s\n", key)

			// Inspect pods in this model using the Array() method
			fmt.Printf("  Pods in model %s: %d pods\n", key, model.Pods.Len())
			if podArray := model.Pods.Array(); podArray != nil {
				for i, pod := range podArray.Pods {
					fmt.Printf("    Pod %d: %s/%s\n", i, pod.Namespace, pod.Name)
				}
			}

			return true
		})

		cache.updatePod(oldPod, newPod)

		// Print all keys in metaModels after updatePod
		fmt.Println("Keys in metaModels after updatePod:")
		cache.metaModels.Range(func(key string, model *Model) bool {
			fmt.Printf("  %s\n", key)

			// Inspect pods in this model using the Array() method
			fmt.Printf("  Pods in model %s: %d pods\n", key, model.Pods.Len())
			if podArray := model.Pods.Array(); podArray != nil {
				for i, pod := range podArray.Pods {
					fmt.Printf("    Pod %d: %s/%s\n", i, pod.Namespace, pod.Name)
				}
			}

			return true
		})

		// OldPod meta deleted - using tenant-aware key format
		oldTenantPodKey := utils.GeneratePodKey("default", "p1", constants.DefaultTenantID)
		_, exist := cache.metaPods.Load(oldTenantPodKey)
		Expect(exist).To(BeFalse())

		// NewPod meta created - using tenant-aware key format
		newTenantPodKey := utils.GeneratePodKey("default", "p2", constants.DefaultTenantID)
		newMetaPod, exist := cache.metaPods.Load(newTenantPodKey)
		Expect(exist).To(BeTrue())
		Expect(newMetaPod.Pod).To(Equal(newPod))
		Expect(newMetaPod.Models.Len()).To(Equal(1))

		// Pod -> Model mapping exists
		_, exist = newMetaPod.Models.Load("m1")
		Expect(exist).To(BeTrue())

		// Pod -> Model adapter cleared
		_, exist = newMetaPod.Models.Load("m1adapter")
		Expect(exist).To(BeFalse())

		// Metrics cleared
		Expect(newMetaPod.Metrics.Len()).To(Equal(0))

		// Model meta exists - using tenant-aware model key
		modelKey := utils.GenerateModelKey("m1", constants.DefaultTenantID)
		metaModel, exist := cache.metaModels.Load(modelKey)
		Expect(exist).To(BeTrue())
		Expect(metaModel).ToNot(BeNil())
		Expect(metaModel.Pods).ToNot(BeNil())
		Expect(metaModel.Pods.Len()).To(Equal(1))

		// Model -> pod mapping exists - using tenant-aware pod key
		podKey := utils.GeneratePodKey("default", "p2", constants.DefaultTenantID)
		modelPod, exist := metaModel.Pods.Load(podKey)
		Expect(exist).To(BeTrue())
		Expect(modelPod).To(Equal(newPod))

		// Model adapter meta should be cleared - using tenant-aware key
		adapterKey := utils.GenerateModelKey("m1adapter", constants.DefaultTenantID)
		_, exist = cache.metaModels.Load(adapterKey)
		Expect(exist).To(BeFalse()) // The adapter should be removed when pod is updated
	})

	It("should pods returned after updatePod reflect updated pods", func() {
		cache := newCache()

		oldPod := getNewPod("p1", "default", "m1", 0)
		cache.addPod(oldPod)
		// Use tenant-aware model key format
		modelKey := utils.GenerateModelKey("m1", constants.DefaultTenantID)
		metaModel, exist := cache.metaModels.Load(modelKey)
		Expect(exist).To(BeTrue())
		pods := metaModel.Pods.Array()
		Expect(pods.Len()).To(Equal(1))
		Expect(utils.CountRoutablePods(pods.All())).To(Equal(0))

		newPod := getReadyPod("p1", "default", "m1", 0) // IP may changed due to migration
		cache.updatePod(oldPod, newPod)

		// After update, check using the same model key
		metaModel, exist = cache.metaModels.Load(modelKey)
		Expect(exist).To(BeTrue())
		pods = metaModel.Pods.Array()
		Expect(pods.Len()).To(Equal(1))
		Expect(utils.CountRoutablePods(pods.All())).To(Equal(1))
	})

	It("should deletePod clear pod, model, and modelAdapter entrys", func() {
		cache := newCache()
		pod := getReadyPod("p1", "default", "m1", 0)
		cache.addPod(pod)
		cache.addModelAdapter(getNewModelAdapter("m1adapter", "default", pod.Name))

		cache.deletePod(pod)

		// Pod meta deleted - using tenant-aware key format
		tenantPodKey := utils.GeneratePodKey("default", "p1", constants.DefaultTenantID)
		_, exist := cache.metaPods.Load(tenantPodKey)
		Expect(exist).To(BeFalse())

		// Related model meta deleted - using tenant-aware model key
		modelKey := utils.GenerateModelKey("m1", constants.DefaultTenantID)
		_, exist = cache.metaModels.Load(modelKey)
		Expect(exist).To(BeFalse())

		// Related model adapter meta deleted - using tenant-aware model key
		adapterKey := utils.GenerateModelKey("m0adapter", constants.DefaultTenantID)
		_, exist = cache.metaModels.Load(adapterKey)
		Expect(exist).To(BeFalse())

		// Abnormal: Pod without model label exists
		cache.addPod(pod)

		// Using tenant-aware key format
		tenantPodKey = utils.GeneratePodKey("default", "p1", constants.DefaultTenantID)
		_, exist = cache.metaPods.Load(tenantPodKey)
		Expect(exist).To(BeTrue())

		pod.ObjectMeta.Labels = nil
		cache.deletePod(pod)
		_, exist = cache.metaPods.Load(tenantPodKey)
		Expect(exist).To(BeFalse())
	})

	It("should deleteModelAdapter remove mappings", func() {
		cache := newCache()
		cache.addPod(getReadyPod("p1", "default", "m1", 0))
		cache.addPod(getReadyPod("p2", "default", "m1", 0))
		cache.addPod(getReadyPod("p3", "default", "m1", 0))
		oldAdapter := getNewModelAdapterWithPods("m1adapter1", "default", []string{"p1", "p2"})
		cache.addModelAdapter(oldAdapter)

		// Debug print all model keys before deletion
		fmt.Println("\nDEBUG - Model keys BEFORE deletion:")
		cache.metaModels.Range(func(key string, model *Model) bool {
			fmt.Printf("  Model key: %s (pods: %d)\n", key, model.Pods.Len())
			return true
		})

		// Debug print all pod model mappings before deletion
		fmt.Println("\nDEBUG - Pod model mappings BEFORE deletion:")
		cache.metaPods.Range(func(key string, pod *Pod) bool {
			fmt.Printf("  Pod key: %s (models: %d)\n", key, pod.Models.Len())

			// Print the actual model names in the pod's Models map
			if pod.Models.Len() > 0 {
				modelArray := pod.Models.Array()
				fmt.Printf("    Model names: %v\n", modelArray)
			}
			return true
		})

		// Debug - print the model adapter we're about to delete
		fmt.Printf("\nDEBUG - Model adapter to delete: %s/%s with instances: %v\n",
			oldAdapter.Namespace, oldAdapter.Name, oldAdapter.Status.Instances)

		cache.deleteModelAdapter(oldAdapter)

		// Debug print all model keys after deletion
		fmt.Println("\nDEBUG - Model keys AFTER deletion:")
		cache.metaModels.Range(func(key string, model *Model) bool {
			fmt.Printf("  Model key: %s (pods: %d)\n", key, model.Pods.Len())
			return true
		})

		// Debug print all pod model mappings after deletion
		fmt.Println("\nDEBUG - Pod model mappings AFTER deletion:")
		cache.metaPods.Range(func(key string, pod *Pod) bool {
			fmt.Printf("  Pod key: %s (models: %d)\n", key, pod.Models.Len())

			// Print the actual model names in the pod's Models map
			if pod.Models.Len() > 0 {
				modelArray := pod.Models.Array()
				fmt.Printf("    Model names: %v\n", modelArray)
			}
			return true
		})

		// Pod1 - using tenant-aware key format
		tenantP1Key := utils.GeneratePodKey("default", "p1", constants.DefaultTenantID)
		p1MetaPod, exist := cache.metaPods.Load(tenantP1Key)
		Expect(exist).To(BeTrue())
		Expect(p1MetaPod.Models.Len()).To(Equal(1))
		_, exist = p1MetaPod.Models.Load("m1adapter1")
		Expect(exist).To(BeFalse(), "m1adapter1 should be removed from p1's models")

		// Pod2 - using tenant-aware key format
		tenantP2Key := utils.GeneratePodKey("default", "p2", constants.DefaultTenantID)
		p2MetaPod, exist := cache.metaPods.Load(tenantP2Key)
		Expect(exist).To(BeTrue())
		Expect(p2MetaPod.Models.Len()).To(Equal(1)) // Include base model
		_, exist = p2MetaPod.Models.Load("m1adapter1")
		Expect(exist).To(BeFalse(), "m1adapter1 should be removed from p2's models")

		// Now check that the model adapter entries are properly cleaned up
		// Check using tenant-aware model key format
		adapterTenantKey := utils.GenerateModelKey("m1adapter1", constants.DefaultTenantID)
		_, existTenant := cache.metaModels.Load(adapterTenantKey)

		Expect(existTenant).To(BeFalse(),
			"Model adapter should be removed from cache (key format: tenant=%s)",
			adapterTenantKey)
	})

	It("should updateModelAdapter reset mappings", func() {
		cache := newCache()
		cache.addPod(getReadyPod("p1", "default", "m1", 0))
		cache.addPod(getReadyPod("p2", "default", "m1", 0))
		cache.addPod(getReadyPod("p3", "default", "m1", 0))
		oldAdapter := getNewModelAdapterWithPods("m1adapter1", "default", []string{"p1", "p2"})
		cache.addModelAdapter(oldAdapter)

		// Debug: Print cache state before update
		fmt.Println("\nDEBUG - Before updateModelAdapter:")
		fmt.Println("Model keys:")
		cache.metaModels.Range(func(key string, model *Model) bool {
			fmt.Printf("  Model key: %s (pods: %d)\n", key, model.Pods.Len())
			if model.Pods.Len() > 0 {
				fmt.Println("  Pods in this model:")
				if podArray := model.Pods.Array(); podArray != nil {
					for i, pod := range podArray.Pods {
						fmt.Printf("    Pod %d: %s/%s\n", i, pod.Namespace, pod.Name)
					}
				}
			}
			return true
		})

		fmt.Println("Pod keys and their models:")
		cache.metaPods.Range(func(key string, pod *Pod) bool {
			fmt.Printf("  Pod key: %s (models: %d)\n", key, pod.Models.Len())
			if pod.Models.Len() > 0 {
				modelArray := pod.Models.Array()
				fmt.Printf("    Model names: %v\n", modelArray)
			}
			return true
		})

		newAdapter := getNewModelAdapterWithPods("m1adapter2", "default", []string{"p2", "p3"})

		cache.updateModelAdapter(oldAdapter, newAdapter)

		// Debug: Print cache state after update
		fmt.Println("\nDEBUG - After updateModelAdapter:")
		fmt.Println("Model keys:")
		cache.metaModels.Range(func(key string, model *Model) bool {
			fmt.Printf("  Model key: %s (pods: %d)\n", key, model.Pods.Len())
			if model.Pods.Len() > 0 {
				fmt.Println("  Pods in this model:")
				if podArray := model.Pods.Array(); podArray != nil {
					for i, pod := range podArray.Pods {
						fmt.Printf("    Pod %d: %s/%s\n", i, pod.Namespace, pod.Name)
					}
				}
			}
			return true
		})

		fmt.Println("Pod keys and their models:")
		cache.metaPods.Range(func(key string, pod *Pod) bool {
			fmt.Printf("  Pod key: %s (models: %d)\n", key, pod.Models.Len())
			if pod.Models.Len() > 0 {
				modelArray := pod.Models.Array()
				fmt.Printf("    Model names: %v\n", modelArray)
			}
			return true
		})

		// Pod1 - using tenant-aware key format
		tenantP1Key := utils.GeneratePodKey("default", "p1", constants.DefaultTenantID)
		p1MetaPod, exist := cache.metaPods.Load(tenantP1Key)
		Expect(exist).To(BeTrue())
		Expect(p1MetaPod.Models.Len()).To(Equal(1))
		_, exist = p1MetaPod.Models.Load("m1adapter1")
		Expect(exist).To(BeFalse())
		_, exist = p1MetaPod.Models.Load("m1adapter2")
		Expect(exist).To(BeFalse())

		// Pod2 - using tenant-aware key format
		tenantP2Key := utils.GeneratePodKey("default", "p2", constants.DefaultTenantID)
		p2MetaPod, exist := cache.metaPods.Load(tenantP2Key)
		Expect(exist).To(BeTrue())
		Expect(p2MetaPod.Models.Len()).To(Equal(2)) // Include base model
		_, exist = p2MetaPod.Models.Load("m1adapter1")
		Expect(exist).To(BeFalse())
		_, exist = p2MetaPod.Models.Load("m1adapter2")
		Expect(exist).To(BeTrue())

		// Pod3 - using tenant-aware key format
		tenantP3Key := utils.GeneratePodKey("default", "p3", constants.DefaultTenantID)
		p3MetaPod, exist := cache.metaPods.Load(tenantP3Key)
		Expect(exist).To(BeTrue())
		Expect(p3MetaPod.Models.Len()).To(Equal(2)) // Include base model
		_, exist = p3MetaPod.Models.Load("m1adapter1")
		Expect(exist).To(BeFalse())
		_, exist = p3MetaPod.Models.Load("m1adapter2")
		Expect(exist).To(BeTrue())

		// Model adpater1 cleared - using tenant-aware model key
		adapter1Key := utils.GenerateModelKey("m1adapter1", "default")
		_, exist = cache.metaModels.Load(adapter1Key)
		Expect(exist).To(BeFalse())

		// Model adpater2 registered - using tenant-aware model key
		adapter2Key := utils.GenerateModelKey("m1adapter2", "default")
		metaModel, exist := cache.metaModels.Load(adapter2Key)
		Expect(exist).To(BeTrue())
		Expect(metaModel).ToNot(BeNil())
		Expect(metaModel.Pods).ToNot(BeNil())
		Expect(metaModel.Pods.Len()).To(Equal(2))

		// Check model -> pod mappings using tenant-aware pod keys
		tenantP1Key = utils.GeneratePodKey("default", "p1", constants.DefaultTenantID)
		tenantP2Key = utils.GeneratePodKey("default", "p2", constants.DefaultTenantID)
		tenantP3Key = utils.GeneratePodKey("default", "p3", constants.DefaultTenantID)

		_, exist = metaModel.Pods.Load(tenantP1Key)
		Expect(exist).To(BeFalse())
		_, exist = metaModel.Pods.Load(tenantP2Key)
		Expect(exist).To(BeTrue())
		_, exist = metaModel.Pods.Load(tenantP3Key)
		Expect(exist).To(BeTrue())
	})

	It("should GetPodByKey return k8s pod", func() {
		cache := newCache()
		pod := getReadyPod("p1", "default", "m1", 0)
		cache.addPod(pod)

		// First lookup uses NewPodKey directly, which expects to find a tenant-aware key in the cache
		_, err := cache.GetPodByKey(utils.NewPodKey("default", "p0", constants.DefaultTenantID))
		Expect(err).ToNot(BeNil())

		// Use tenant-aware key to verify pod is stored with tenant-aware key
		tenantPodKey := utils.GeneratePodKey(pod.Namespace, pod.Name, constants.DefaultTenantID)
		metaPod, exist := cache.metaPods.Load(tenantPodKey)
		Expect(exist).To(BeTrue())
		Expect(metaPod.Pod).To(Equal(pod))

		// Now the lookup should work directly with tenant-aware key
		key := utils.NewPodKey(pod.Namespace, pod.Name, constants.DefaultTenantID)
		actual, err := cache.GetPodByKey(key)
		Expect(err).To(BeNil())
		Expect(actual).To(BeIdenticalTo(pod))
	})

	It("should GetPods return k8s pod slice", func() {
		cache := newCache()
		pod1 := getReadyPod("p1", "default", "m1", 0)
		pod2 := getReadyPod("p2", "default", "m2", 0)
		cache.addPod(pod1)
		cache.addPod(pod2)

		pods := cache.ListPods()
		Expect(pods).To(HaveLen(2)) // Must be slice
		Expect(pods).To(ContainElement(HaveField("ObjectMeta.Name", "p1")))
		Expect(pods).To(ContainElement(HaveField("ObjectMeta.Name", "p2")))
	})

	It("should GetPodsForModel() return a PodArray", func() {
		cache := newCache()
		pod1 := getReadyPod("p1", "default", "m1", 0)
		pod2 := getReadyPod("p2", "default", "m2", 0)
		cache.addPod(pod1)
		cache.addPod(pod2)

		// Non-existent model should return error
		_, err := cache.ListPodsByModelKey(utils.NewModelKey("m0", "default"))
		Expect(err).ToNot(BeNil())

		// For models, we need to use tenant-aware keys for both the test and in the implementation
		modelKey := utils.GenerateModelKey("m1", "default")

		// Verify that model exists with tenant-aware key
		metaModel, exists := cache.metaModels.Load(modelKey)
		Expect(exists).To(BeTrue())

		// Check that pods are correctly mapped in the model
		tenantPodKey := utils.GeneratePodKey(constants.DefaultTenantID, "p1", constants.DefaultTenantID)
		_, exists = metaModel.Pods.Load(tenantPodKey)
		Expect(exists).To(BeTrue())

		// Now the lookup should work
		pods, err := cache.ListPodsByModelKey(utils.NewModelKey("m1", constants.DefaultTenantID))
		Expect(err).To(BeNil())
		Expect(pods.Len()).To(Equal(1))
		Expect(pods.All()).To(HaveLen(1))
		Expect(pods.All()).To(ContainElement(HaveField("ObjectMeta.Name", "p1")))
	})

	It("should ListModels return string slice", func() {
		cache := newCache()
		pod1 := getReadyPod("p1", "default", "m1", 0)
		pod2 := getReadyPod("p2", "default", "m2", 0)
		cache.addPod(pod1)
		cache.addPod(pod2)

		// Verify that models exist with tenant-aware keys
		modelKey1 := utils.GenerateModelKey("m1", "default")
		modelKey2 := utils.GenerateModelKey("m2", "default")

		_, exist := cache.metaModels.Load(modelKey1)
		Expect(exist).To(BeTrue())

		_, exist = cache.metaModels.Load(modelKey2)
		Expect(exist).To(BeTrue())

		// Model lookups use tenant-aware keys
		exist = cache.HasModelKey(utils.NewModelKey("m0", "default"))
		Expect(exist).To(BeFalse())

		exist = cache.HasModelKey(utils.NewModelKey("m1", "default"))
		Expect(exist).To(BeTrue())

		// ListModels by tenant should work correctly
		models := cache.ListModels("default")
		Expect(models).To(HaveLen(2)) // Must be slice
		Expect(models).To(ContainElement("m1"))
		Expect(models).To(ContainElement("m2"))
	})

	It("should GetModelsForPod() return a string slice", func() {
		cache := newCache()
		pod1 := getReadyPod("p1", "default", "m1", 0)
		pod2 := getReadyPod("p2", "default", "m2", 0)
		cache.addPod(pod1)
		cache.addPod(pod2)

		// This will fail for a non-existent pod
		_, err := cache.ListModelsByPodKey(utils.NewPodKey("default", "p0", constants.DefaultTenantID))
		Expect(err).ToNot(BeNil())

		// For an existing pod, we should be able to look it up directly with tenant-aware key
		key := utils.NewPodKey(pod1.Namespace, pod1.Name, constants.DefaultTenantID)
		tenantPodKey := utils.GeneratePodKey(pod1.Namespace, pod1.Name, constants.DefaultTenantID)
		_, exist := cache.metaPods.Load(tenantPodKey)
		Expect(exist).To(BeTrue())

		// Now the lookup should work
		models, err := cache.ListModelsByPodKey(key)
		Expect(err).To(BeNil())
		Expect(models).To(HaveLen(1))
		Expect(models).To(ContainElement("m1"))
	})

	It("should basic add request count, add request trace no err", func() {
		modelName := "llama-7b"
		tenantID := constants.DefaultTenantID
		cache := newTraceCache()
		cache.AddPod(getReadyPod("p1", "default", modelName, 0))

		// Verify model exists with tenant-aware key
		modelKeyStr := utils.GenerateModelKey(modelName, tenantID)
		_, exist := cache.metaModels.Load(modelKeyStr)
		Expect(exist).To(BeTrue())

		// Add request count
		modelKey := utils.NewModelKey(modelName, tenantID)
		term := cache.AddRequestCountByModelKey(nil, "no use now", modelKey)
		Expect(cache.numRequestsTraces).To(Equal(int32(1)))

		// Get trace - the implementation uses the tenant-aware key format
		trace := cache.getRequestTrace(modelKeyStr)
		Expect(trace).ToNot(BeNil())
		Expect(trace.numKeys).To(Equal(int32(0)))
		Expect(trace.numRequests).To(Equal(int32(1)))
		Expect(trace.completedRequests).To(Equal(int32(0)))

		meta, exist := cache.metaModels.Load(modelKeyStr)
		Expect(exist).To(BeTrue())
		Expect(meta.pendingRequests).To(Equal(int32(1)))

		// Done request count
		cache.DoneRequestCountByModelKey(nil, "no use now", modelKey, term)
		Expect(cache.numRequestsTraces).To(Equal(int32(1)))
		trace = cache.getRequestTrace(modelKeyStr)
		Expect(trace).ToNot(BeNil())
		Expect(trace.numRequests).To(Equal(int32(1)))
		Expect(trace.completedRequests).To(Equal(int32(1)))
		meta, exist = cache.metaModels.Load(modelKeyStr)
		Expect(exist).To(BeTrue())
		Expect(meta.pendingRequests).To(Equal(int32(0)))

		// Done request trace
		cache.DoneRequestTraceByModelKey(nil, "no use now", modelKey, 1, 1, 1)
		Expect(trace.numKeys).To(Equal(int32(1)))
		pProfileCounter, exist := trace.trace.Load("0:0") // log2(1)
		Expect(exist).To(BeTrue())
		Expect(*pProfileCounter.(*int32)).To(Equal(int32(1)))
	})

	It("should global pending counter return 0.", func() {
		modelName := "llama-7b"
		tenantID := constants.DefaultTenantID
		cache := newTraceCache()
		cache.AddPod(getReadyPod("p1", "default", modelName, 0))

		// Verify model exists with tenant-aware key
		modelKeyStr := utils.GenerateModelKey(modelName, tenantID)
		_, exist := cache.metaModels.Load(modelKeyStr)
		Expect(exist).To(BeTrue())

		total := 100
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ { // Reduced count to speed up test
			wg.Add(1)
			go func() {
				for j := 0; j < total; j++ {
					// Retry until success
					term := cache.AddRequestCountByModelKey(nil, "no use now", utils.NewModelKey(modelName, tenantID))
					runtime.Gosched()
					cache.DoneRequestTraceByModelKey(nil, "no use now", utils.NewModelKey(modelName, tenantID), 1, 1, term)
				}
				wg.Done()
			}()
		}
		wg.Wait()

		meta, _ := cache.metaModels.Load(modelKeyStr)
		Expect(atomic.LoadInt32(&meta.pendingRequests)).To(Equal(int32(0)))
	})
})

func BenchmarkLagacyAddRequestTrace(b *testing.B) {
	cache := &lagacyCache{
		requestTrace: map[string]map[string]int{},
	}
	thread := 10
	var wg sync.WaitGroup
	for i := 0; i < thread; i++ {
		wg.Add(1)
		go func() {
			for i := 0; i < b.N/thread; i++ {
				cache.AddRequestTrace("model", rand.Int63n(8192), rand.Int63n(1024))
			}
			wg.Done()
		}()
	}
	wg.Wait()
}

func BenchmarkAddRequest(b *testing.B) {
	cache := newTraceCache()
	thread := 10
	var wg sync.WaitGroup
	for i := 0; i < thread; i++ {
		wg.Add(1)
		go func() {
			for i := 0; i < b.N/thread; i++ {
				cache.AddRequestCountByModelKey(nil, "no use now", utils.NewModelKey("model", "default"))
			}
			wg.Done()
		}()
	}
	wg.Wait()
}

func BenchmarkDoneRequest(b *testing.B) {
	cache := newTraceCache()
	thread := 10
	var wg sync.WaitGroup
	term := cache.AddRequestCountByModelKey(nil, "no use now", utils.NewModelKey("model", "default"))
	for i := 0; i < thread; i++ {
		wg.Add(1)
		go func() {
			for i := 0; i < b.N/thread; i++ {
				cache.DoneRequestCountByModelKey(nil, "no use now", utils.NewModelKey("model", "default"), term)
			}
			wg.Done()
		}()
	}
	wg.Wait()
}

func BenchmarkDoneRequestTrace(b *testing.B) {
	cache := newTraceCache()
	thread := 10
	var wg sync.WaitGroup
	term := cache.AddRequestCountByModelKey(nil, "no use now", utils.NewModelKey("model", "default"))
	for i := 0; i < thread; i++ {
		wg.Add(1)
		go func() {
			for i := 0; i < b.N/thread; i++ {
				cache.DoneRequestTraceByModelKey(nil, "no use now", utils.NewModelKey("model", "default"), rand.Int63n(8192), rand.Int63n(1024), term)
			}
			wg.Done()
		}()
	}
	wg.Wait()
}
