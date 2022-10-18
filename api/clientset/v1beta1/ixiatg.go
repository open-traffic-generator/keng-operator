/*
Copyright 2021.

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

package v1beta1

import (
	"context"
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"

	typesv1beta1 "github.com/open-traffic-generator/ixia-c-operator/api/v1beta1"
)

// IxiaTGInterface provides access to the IxiaTG CRD.
type IxiaTGInterface interface {
	List(ctx context.Context, opts metav1.ListOptions) (*typesv1beta1.IxiaTGList, error)
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*typesv1beta1.IxiaTG, error)
	Create(ctx context.Context, ixiaTG *typesv1beta1.IxiaTG) (*typesv1beta1.IxiaTG, error)
	Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error
	Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error)
	Unstructured(ctx context.Context, name string, opts metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error)
	Update(ctx context.Context, obj *unstructured.Unstructured, opts metav1.UpdateOptions) (*typesv1beta1.IxiaTG, error)
}

// Interface is the clientset interface for IxiaTG.
type Interface interface {
	IxiaTG(namespace string) IxiaTGInterface
}

// Clientset is a client for the ixiatg crds.
type Clientset struct {
	dInterface dynamic.NamespaceableResourceInterface
	restClient rest.Interface
}

func GVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    typesv1beta1.GroupName,
		Version:  typesv1beta1.GroupVersion,
		Resource: "ixiatgs",
	}
}

func GV() *schema.GroupVersion {
	return &schema.GroupVersion{
		Group:   typesv1beta1.GroupName,
		Version: typesv1beta1.GroupVersion,
	}
}

// NewForConfig returns a new Clientset based on c.
func NewForConfig(c *rest.Config) (*Clientset, error) {
	config := *c
	config.ContentConfig.GroupVersion = &schema.GroupVersion{Group: GVR().Group, Version: GVR().Version}
	config.APIPath = "/apis"
	config.NegotiatedSerializer = scheme.Codecs.WithoutConversion()
	config.UserAgent = rest.DefaultKubernetesUserAgent()

	dClient, err := dynamic.NewForConfig(c)
	if err != nil {
		return nil, err
	}

	dInterface := dClient.Resource(GVR())

	rClient, err := rest.RESTClientFor(&config)
	if err != nil {
		return nil, err
	}

	return &Clientset{
		dInterface: dInterface,
		restClient: rClient,
	}, nil
}

// IxiaTG initializes IxiaTGClient struct which implements IxiaTGInterface.
func (c *Clientset) IxiaTG(namespace string) IxiaTGInterface {
	return &IxiaTGClient{
		dInterface: c.dInterface,
		restClient: c.restClient,
		ns:         namespace,
	}
}

type IxiaTGClient struct {
	dInterface dynamic.NamespaceableResourceInterface
	restClient rest.Interface
	ns         string
}

// List gets a list of IxiaTG resources.
func (i *IxiaTGClient) List(
	ctx context.Context,
	opts metav1.ListOptions, // skipcq: CRT-P0003
) (*typesv1beta1.IxiaTGList, error) {
	result := typesv1beta1.IxiaTGList{}
	err := i.restClient.
		Get().
		Namespace(i.ns).
		Resource(GVR().Resource).
		VersionedParams(&opts, scheme.ParameterCodec).
		Do(ctx).
		Into(&result)

	return &result, err
}

// Get gets IxiaTG resource.
func (i *IxiaTGClient) Get(
	ctx context.Context,
	name string,
	opts metav1.GetOptions,
) (*typesv1beta1.IxiaTG, error) {
	result := typesv1beta1.IxiaTG{}
	err := i.restClient.
		Get().
		Namespace(i.ns).
		Resource(GVR().Resource).
		Name(name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Do(ctx).
		Into(&result)

	return &result, err
}

// Create creates IxiaTG resource.
func (i *IxiaTGClient) Create(
	ctx context.Context,
	ixiaTG *typesv1beta1.IxiaTG,
) (*typesv1beta1.IxiaTG, error) {
	result := typesv1beta1.IxiaTG{}
	err := i.restClient.
		Post().
		Namespace(i.ns).
		Resource(GVR().Resource).
		Body(ixiaTG).
		Do(ctx).
		Into(&result)

	return &result, err
}

func (i *IxiaTGClient) Watch(
	ctx context.Context,
	opts metav1.ListOptions, // skipcq: CRT-P0003
) (watch.Interface, error) {
	opts.Watch = true

	return i.restClient.
		Get().
		Namespace(i.ns).
		Resource(GVR().Resource).
		VersionedParams(&opts, scheme.ParameterCodec).
		Watch(ctx)
}

func (i *IxiaTGClient) Delete(ctx context.Context,
	name string,
	opts metav1.DeleteOptions, // skipcq: CRT-P0003
) error {
	return i.restClient.
		Delete().
		Namespace(i.ns).
		Resource(GVR().Resource).
		VersionedParams(&opts, scheme.ParameterCodec).
		Name(name).
		Do(ctx).
		Error()
}

func (i *IxiaTGClient) Update(
	ctx context.Context,
	obj *unstructured.Unstructured,
	opts metav1.UpdateOptions,
) (*typesv1beta1.IxiaTG, error) {
	result := typesv1beta1.IxiaTG{}

	err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), &result)
	if err != nil {
		return nil, err
	}

	crdBytes, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}

	err = i.restClient.
		Patch(types.MergePatchType).
		Namespace(i.ns).
		Resource(GVR().Resource).
		VersionedParams(&opts, scheme.ParameterCodec).
		Name(result.Name).
		Body(crdBytes).
		Do(ctx).
		Error()
	if err != nil {
		return nil, err
	}

	return &result, nil
}

func (i *IxiaTGClient) Unstructured(ctx context.Context, name string, opts metav1.GetOptions,
	subresources ...string,
) (*unstructured.Unstructured, error) {
	return i.dInterface.Namespace(i.ns).Get(ctx, name, opts, subresources...)
}

func init() {
	if err := typesv1beta1.AddToScheme(scheme.Scheme); err != nil {
		panic(err)
	}
}
