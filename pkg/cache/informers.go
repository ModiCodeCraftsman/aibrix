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
	"errors"
	"strings"

	crdinformers "github.com/vllm-project/aibrix/pkg/client/informers/externalversions"
	"github.com/vllm-project/aibrix/pkg/constants"
	"github.com/vllm-project/aibrix/pkg/utils"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	modelv1alpha1 "github.com/vllm-project/aibrix/api/model/v1alpha1"
	v1alpha1 "github.com/vllm-project/aibrix/pkg/client/clientset/versioned"
	v1alpha1scheme "github.com/vllm-project/aibrix/pkg/client/clientset/versioned/scheme"
	"k8s.io/client-go/kubernetes/scheme"
)

const (
	modelIdentifier = "model.aibrix.ai/name"
	nodeType        = "ray.io/node-type"
	nodeWorker      = "worker"
)

func initCacheInformers(instance *Store, config *rest.Config, stopCh <-chan struct{}) error {
	if err := v1alpha1scheme.AddToScheme(scheme.Scheme); err != nil {
		return err
	}

	k8sClientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}

	crdClientSet, err := v1alpha1.NewForConfig(config)
	if err != nil {
		return err
	}

	factory := informers.NewSharedInformerFactoryWithOptions(k8sClientSet, 0)
	crdFactory := crdinformers.NewSharedInformerFactoryWithOptions(crdClientSet, 0)

	podInformer := factory.Core().V1().Pods().Informer()
	if _, err := podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    instance.addPod,
		UpdateFunc: instance.updatePod,
		DeleteFunc: instance.deletePod,
	}); err != nil {
		return err
	}

	modelInformer := crdFactory.Model().V1alpha1().ModelAdapters().Informer()
	if _, err = modelInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    instance.addModelAdapter,
		UpdateFunc: instance.updateModelAdapter,
		DeleteFunc: instance.deleteModelAdapter,
	}); err != nil {
		return err
	}

	factory.Start(stopCh)
	crdFactory.Start(stopCh)

	if !cache.WaitForCacheSync(stopCh, podInformer.HasSynced, modelInformer.HasSynced) {
		return errors.New("timed out waiting for caches to sync")
	}

	// After cache sync, resync all ModelAdapters to ensure pod mappings are correct
	// This handles the case where ModelAdapters were processed before their pods were cached
	instance.resyncModelAdapters(modelInformer.GetStore())

	// Log cache state after initialization
	// TODO: What's the intention here? if we want to print all models we should iterate over th entire cache
	klog.Infof("Cache initialization completed. Models: %v", instance.ListModels(constants.DefaultTenantID))

	return nil
}

func (c *Store) addPod(obj interface{}) {
	pod := obj.(*v1.Pod)
	// only track pods with model deployments
	modelName, ok := pod.Labels[modelIdentifier]
	if !ok {
		return
	}
	// ignore worker pods
	nodeType, ok := pod.Labels[nodeType]
	if ok && nodeType == nodeWorker {
		klog.InfoS("ignored ray worker pod", "name", pod.Name)
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	metaPod := c.addPodLocked(pod)
	c.addPodAndModelMappingLocked(metaPod, modelName)

	klog.V(4).Infof("POD CREATED: %s/%s", pod.Namespace, pod.Name)
	c.debugInfo()
}

func (c *Store) updatePod(oldObj interface{}, newObj interface{}) {
	oldPod := oldObj.(*v1.Pod)
	newPod := newObj.(*v1.Pod)

	_, oldOk := oldPod.Labels[modelIdentifier]
	tenantKey := utils.GeneratePodKey(oldPod.Namespace, oldPod.Name, constants.DefaultTenantID)
	_, existed := c.metaPods.Load(tenantKey) // Make sure nothing left.
	newModelName, newOk := newPod.Labels[modelIdentifier]

	if !oldOk && !existed && !newOk {
		return // No model information to track in either old or new pod
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// TODO: No in place update is handled here

	// Remove old mappings if present, no adapter will be inherited. (Adapters will be rescaned and readded later)
	if oldOk || existed {
		// First, find all model adapters associated with this pod and clean them up
		// This code checks for model adapters specifically (names ending with "adapter")
		c.metaModels.Range(func(modelKey string, model *Model) bool {
			// Check if this is a model adapter (e.g., ends with "adapter")
			parts := strings.Split(modelKey, "/")
			if len(parts) > 1 && strings.HasSuffix(parts[1], "adapter") {
				// Get the list of pods in this model adapter
				podArray := model.Pods.Array()
				if podArray != nil {
					podsToDelete := []string{}

					// For each pod in the array
					for _, pod := range podArray.Pods {
						if pod.Name == oldPod.Name && pod.Namespace == oldPod.Namespace {
							// Mark this pod for deletion from this model adapter
							// We need to find the actual key used to store it
							if podKey := findPodKeyInModel(model, pod); podKey != "" {
								podsToDelete = append(podsToDelete, podKey)
							}
						}
					}

					// Delete all identified pods from this model adapter
					for _, podKey := range podsToDelete {
						model.Pods.Delete(podKey)
					}

					// If no pods left, we could clean up the model entry completely
					if model.Pods.Len() == 0 {
						c.metaModels.Delete(modelKey)
					}
				}
			}
			return true
		})

		// Now delete the pod and its regular model mappings
		odlMetaPod := c.deletePodLocked(oldPod.Name, oldPod.Namespace, constants.DefaultTenantID)
		if odlMetaPod != nil {
			for _, modelName := range odlMetaPod.Models.Array() {
				c.deletePodAndModelMappingLocked(odlMetaPod.Name, odlMetaPod.Namespace, modelName, 1, constants.DefaultTenantID)
			}
		}
	}

	// ignore worker pods
	oldNodeType := oldPod.Labels[nodeType]
	newNodeType := newPod.Labels[nodeType]
	if oldNodeType == nodeWorker || newNodeType == nodeWorker {
		klog.InfoS("ignored ray worker pod", "old pod", oldPod.Name, "new pod", newPod.Name)
		return
	}

	// Add new mappings if present
	if newOk {
		metaPod := c.addPodLocked(newPod)
		c.addPodAndModelMappingLocked(metaPod, newModelName)
	}

	klog.V(4).Infof("POD UPDATED: %s/%s %s", newPod.Namespace, newPod.Name, newPod.Status.Phase)
	c.debugInfo()
}

func (c *Store) deletePod(obj interface{}) {
	var namespace, name string
	var hasModelLabel bool
	switch obj := obj.(type) {
	case *v1.Pod:
		namespace, name = obj.Namespace, obj.Name
		_, hasModelLabel = obj.Labels[modelIdentifier]
	case cache.DeletedFinalStateUnknown:
		if pod, ok := obj.Obj.(*v1.Pod); ok {
			namespace, name = pod.Namespace, pod.Name
			_, hasModelLabel = pod.Labels[modelIdentifier]
			break
		}

		// We can usually ignore cases where the object contained in the tombstone isn't a *v1.Pod. However,
		// since the following logic in this function still works fine with just the namespace and name,
		// let's try parsing the tombstone.Key here for added robustness.
		var err error
		namespace, name, err = cache.SplitMetaNamespaceKey(obj.Key)
		if err != nil {
			klog.ErrorS(err, "couldn't get pod's namespace and name from tombstone", "key", obj.Key)
			return
		}
	}
	tenantKey := utils.GeneratePodKey(namespace, name, constants.DefaultTenantID)
	_, existed := c.metaPods.Load(tenantKey)
	if !hasModelLabel && !existed {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// delete base model and associated lora models on this pod
	metaPod := c.deletePodLocked(name, namespace, constants.DefaultTenantID)
	if metaPod != nil {
		for _, modelName := range metaPod.Models.Array() {
			c.deletePodAndModelMappingLocked(name, namespace, modelName, 1, constants.DefaultTenantID)
		}
	}

	klog.V(4).Infof("POD DELETED: %s/%s", namespace, name)
	c.debugInfo()
}

func (c *Store) addModelAdapter(obj interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()

	model := obj.(*modelv1alpha1.ModelAdapter)
	for _, pod := range model.Status.Instances {
		c.addPodAndModelMappingLockedByName(pod, model.Namespace, model.Name)
	}

	klog.V(4).Infof("MODELADAPTER CREATED: %s/%s", model.Namespace, model.Name)
	c.debugInfo()
}

func (c *Store) updateModelAdapter(oldObj interface{}, newObj interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()

	oldModel := oldObj.(*modelv1alpha1.ModelAdapter)
	newModel := newObj.(*modelv1alpha1.ModelAdapter)

	// Remove old mappings first
	for _, pod := range oldModel.Status.Instances {
		// the namespace of the pod is same as the namespace of model
		// Use only the standard tenant format
		c.deletePodAndModelMappingLocked(pod, oldModel.Namespace, oldModel.Name, 0, constants.DefaultTenantID)
	}

	// Add new mappings
	for _, pod := range newModel.Status.Instances {
		c.addPodAndModelMappingLockedByName(pod, newModel.Namespace, newModel.Name)
	}

	klog.V(4).Infof("MODELADAPTER UPDATED. %s/%s %s", oldModel.Namespace, oldModel.Name, newModel.Status.Phase)
	c.debugInfo()
}

func (c *Store) deleteModelAdapter(obj interface{}) {
	model, ok := obj.(*modelv1alpha1.ModelAdapter)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return
		}
		model, ok = tombstone.Obj.(*modelv1alpha1.ModelAdapter)
		if !ok {
			return
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// For debugging purposes, log the adapter and its instances
	klog.V(4).Infof("DELETING MODELADAPTER: %s/%s with instances: %v",
		model.Namespace, model.Name, model.Status.Instances)

	for _, pod := range model.Status.Instances {
		// Use the consistent tenant-aware key format to delete mappings
		c.deletePodAndModelMappingLocked(pod, model.Namespace, model.Name, 0, constants.DefaultTenantID)
	}

	klog.V(4).Infof("MODELADAPTER DELETED: %s/%s", model.Namespace, model.Name)
	c.debugInfo()
}

func (c *Store) addPodLocked(pod *v1.Pod) *Pod {
	if c.bufferPod == nil {
		c.bufferPod = &Pod{
			Pod:    pod,
			Models: utils.NewRegistry[string](),
		}
	} else {
		c.bufferPod.Pod = pod
	}

	// Store using tenant-aware key format (tenant/namespace/name)
	tenantKey := utils.GeneratePodKey(pod.Namespace, pod.Name, constants.DefaultTenantID)
	metaPod, loaded := c.metaPods.LoadOrStore(tenantKey, c.bufferPod)
	if !loaded {
		c.bufferPod = nil
	}

	return metaPod
}

func (c *Store) addPodAndModelMappingLockedByName(podName, namespace, modelName string) {
	// Only look up using tenant-aware key
	key := utils.GeneratePodKey(namespace, podName, constants.DefaultTenantID)
	pod, ok := c.metaPods.Load(key)
	if !ok {
		klog.Errorf("pod %s does not exist in internal-cache", podName)
		return
	}

	c.addPodAndModelMappingLocked(pod, modelName)
}

func (c *Store) addPodAndModelMappingLocked(metaPod *Pod, modelName string) {
	if c.bufferModel == nil {
		c.bufferModel = &Model{
			Pods: utils.NewRegistryWithArrayProvider(func(arr []*v1.Pod) *utils.PodArray { return &utils.PodArray{Pods: arr} }),
		}
	}

	// Extract pod details
	namespace := metaPod.Pod.Namespace
	name := metaPod.Pod.Name

	// Use only the standard tenant format for model and pod keys
	tenantID := constants.DefaultTenantID
	modelKey := utils.GenerateModelKey(modelName, tenantID)
	model, loaded := c.metaModels.LoadOrStore(modelKey, c.bufferModel)
	if !loaded {
		// Need to create a new buffer since we used this one
		c.bufferModel = nil
	}

	// Add the pod->model mapping
	metaPod.Models.Store(modelName, modelName)

	// Add the model->pod mapping using tenant-aware pod key
	podKey := utils.GeneratePodKey(namespace, name, tenantID)
	model.Pods.Store(podKey, metaPod.Pod)

	klog.V(5).Infof("Added model mapping: pod=%s/%s, model=%s with tenant: %s",
		namespace, name, modelName, tenantID)
}

func (c *Store) deletePodLocked(podName, podNamespace string, tenantID string) *Pod {
	if tenantID == "" {
		tenantID = constants.DefaultTenantID
	}

	// Look up the pod using tenant-aware key format
	tenantKey := utils.GeneratePodKey(podNamespace, podName, tenantID)
	metaPod, _ := c.metaPods.Load(tenantKey)

	// Delete the pod from the cache
	c.metaPods.Delete(tenantKey)

	return metaPod
}

// deletePodAndModelMapping delete mappings between pods and model by specified names.
// If ignoreMapping > 0, podToModel mapping will be ignored.
// If ignoreMapping < 0, modelToPod mapping will be ignored
func (c *Store) deletePodAndModelMappingLocked(podName, namespace, modelName string, ignoreMapping int, tenantID string) {
	if tenantID == "" {
		tenantID = constants.DefaultTenantID
	}

	if ignoreMapping <= 0 {
		// Handle pod -> model mapping
		tenantKey := utils.GeneratePodKey(namespace, podName, tenantID)
		if metaPod, ok := c.metaPods.Load(tenantKey); ok {
			metaPod.Models.Delete(modelName)
		}
	}

	if ignoreMapping >= 0 {
		// Handle model -> pod mapping using only tenant-aware model key
		modelKey := utils.GenerateModelKey(modelName, constants.DefaultTenantID)
		if meta, ok := c.metaModels.Load(modelKey); ok {
			// Delete pod using tenant-aware key
			podKey := utils.GeneratePodKey(namespace, podName, constants.DefaultTenantID)
			meta.Pods.Delete(podKey)

			if meta.Pods.Len() == 0 {
				c.metaModels.Delete(modelKey)
			}
		}
	}
}

// resyncModelAdapters processes all ModelAdapters from the informer store to ensure
// all pod mappings are correctly established after cache initialization
func (c *Store) resyncModelAdapters(store cache.Store) {
	klog.Info("Resyncing ModelAdapters to ensure pod mappings are correct")

	objects := store.List()
	for _, obj := range objects {
		if modelAdapter, ok := obj.(*modelv1alpha1.ModelAdapter); ok {
			c.mu.Lock()
			// Process each pod instance in the ModelAdapter
			for _, podName := range modelAdapter.Status.Instances {
				// Use tenant-aware key format only
				tenantAwareKey := utils.GeneratePodKey(modelAdapter.Namespace, podName, constants.DefaultTenantID)

				if _, exists := c.metaPods.Load(tenantAwareKey); exists {
					c.addPodAndModelMappingLockedByName(podName, modelAdapter.Namespace, modelAdapter.Name)
					klog.V(4).Infof("Resynced pod mapping for adapter %s/%s, pod %s",
						modelAdapter.Namespace, modelAdapter.Name, podName)
				} else {
					klog.Warningf("Pod %s not found in cache for ModelAdapter %s/%s during resync",
						podName, modelAdapter.Namespace, modelAdapter.Name)
				}
			}
			c.mu.Unlock()
		}
	}

	klog.Info("ModelAdapter resync completed")
}

// Helper function to find the key under which a pod is stored in a model
func findPodKeyInModel(model *Model, targetPod *v1.Pod) string {
	// Just use the standard tenant-aware key format
	podKey := utils.GeneratePodKey(targetPod.Namespace, targetPod.Name, constants.DefaultTenantID)
	if pod, exists := model.Pods.Load(podKey); exists && pod.Name == targetPod.Name {
		return podKey
	}

	// If not found, return empty string
	return ""
}
