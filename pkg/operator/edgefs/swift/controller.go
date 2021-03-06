/*
Copyright 2016 The Rook Authors. All rights reserved.

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

// scale-out, multi-cloud OpenStack/SWIFT services controller
package swift

import (
	"fmt"
	"reflect"

	"github.com/coreos/pkg/capnslog"
	opkit "github.com/rook/operator-kit"
	edgefsv1alpha1 "github.com/rook/rook/pkg/apis/edgefs.rook.io/v1alpha1"
	rookalpha "github.com/rook/rook/pkg/apis/rook.io/v1alpha2"
	"github.com/rook/rook/pkg/clusterd"
	"k8s.io/api/core/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

const (
	customResourceName       = "swift"
	customResourceNamePlural = "swifts"
)

var logger = capnslog.NewPackageLogger("github.com/rook/rook", "edgefs-op-swift")

// SWIFTResource represents the swift custom resource
var SWIFTResource = opkit.CustomResource{
	Name:    customResourceName,
	Plural:  customResourceNamePlural,
	Group:   edgefsv1alpha1.CustomResourceGroup,
	Version: edgefsv1alpha1.Version,
	Scope:   apiextensionsv1beta1.NamespaceScoped,
	Kind:    reflect.TypeOf(edgefsv1alpha1.SWIFT{}).Name(),
}

// SWIFTController represents a controller object for swift custom resources
type SWIFTController struct {
	context         *clusterd.Context
	rookImage       string
	hostNetwork     bool
	dataDirHostPath string
	dataVolumeSize  resource.Quantity
	placement       rookalpha.Placement
	resources       v1.ResourceRequirements
	resourceProfile string
	ownerRef        metav1.OwnerReference
}

// NewSWIFTController create controller for watching SWIFT custom resources created
func NewSWIFTController(
	context *clusterd.Context, rookImage string,
	hostNetwork bool,
	dataDirHostPath string,
	dataVolumeSize resource.Quantity,
	placement rookalpha.Placement,
	resources v1.ResourceRequirements,
	resourceProfile string,
	ownerRef metav1.OwnerReference,
) *SWIFTController {
	return &SWIFTController{
		context:         context,
		rookImage:       rookImage,
		hostNetwork:     hostNetwork,
		dataDirHostPath: dataDirHostPath,
		dataVolumeSize:  dataVolumeSize,
		placement:       placement,
		resources:       resources,
		resourceProfile: resourceProfile,
		ownerRef:        ownerRef,
	}
}

// StartWatch watches for instances of SWIFT custom resources and acts on them
func (c *SWIFTController) StartWatch(namespace string, stopCh chan struct{}) error {

	resourceHandlerFuncs := cache.ResourceEventHandlerFuncs{
		AddFunc:    c.onAdd,
		UpdateFunc: c.onUpdate,
		DeleteFunc: c.onDelete,
	}

	logger.Infof("start watching swift resources in namespace %s", namespace)
	watcher := opkit.NewWatcher(SWIFTResource, namespace, resourceHandlerFuncs, c.context.RookClientset.EdgefsV1alpha1().RESTClient())
	go watcher.Watch(&edgefsv1alpha1.SWIFT{}, stopCh)

	return nil
}

func (c *SWIFTController) onAdd(obj interface{}) {
	swift, err := getSWIFTObject(obj)
	if err != nil {
		logger.Errorf("failed to get swift object: %+v", err)
		return
	}

	if err = c.CreateService(*swift, c.serviceOwners(swift)); err != nil {
		logger.Errorf("failed to create swift %s. %+v", swift.Name, err)
	}
}

func (c *SWIFTController) onUpdate(oldObj, newObj interface{}) {
	oldService, err := getSWIFTObject(oldObj)
	if err != nil {
		logger.Errorf("failed to get old swift object: %+v", err)
		return
	}
	newService, err := getSWIFTObject(newObj)
	if err != nil {
		logger.Errorf("failed to get new swift object: %+v", err)
		return
	}

	if !serviceChanged(oldService.Spec, newService.Spec) {
		logger.Debugf("swift %s did not change", newService.Name)
		return
	}

	logger.Infof("applying swift %s changes", newService.Name)
	if err = c.UpdateService(*newService, c.serviceOwners(newService)); err != nil {
		logger.Errorf("failed to create (modify) swift %s. %+v", newService.Name, err)
	}
}

func (c *SWIFTController) onDelete(obj interface{}) {
	swift, err := getSWIFTObject(obj)
	if err != nil {
		logger.Errorf("failed to get swift object: %+v", err)
		return
	}

	if err = c.DeleteService(*swift); err != nil {
		logger.Errorf("failed to delete swift %s. %+v", swift.Name, err)
	}
}

func (c *SWIFTController) serviceOwners(service *edgefsv1alpha1.SWIFT) []metav1.OwnerReference {
	// Only set the cluster crd as the owner of the SWIFT resources.
	// If the SWIFT crd is deleted, the operator will explicitly remove the SWIFT resources.
	// If the SWIFT crd still exists when the cluster crd is deleted, this will make sure the SWIFT
	// resources are cleaned up.
	return []metav1.OwnerReference{c.ownerRef}
}

func serviceChanged(oldService, newService edgefsv1alpha1.SWIFTSpec) bool {
	return false
}

func getSWIFTObject(obj interface{}) (swift *edgefsv1alpha1.SWIFT, err error) {
	var ok bool
	swift, ok = obj.(*edgefsv1alpha1.SWIFT)
	if ok {
		// the swift object is of the latest type, simply return it
		return swift.DeepCopy(), nil
	}

	return nil, fmt.Errorf("not a known swift object: %+v", obj)
}
