/*
Copyright The Kubernetes Authors.

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

// Code generated by client-gen. DO NOT EDIT.

package v1

import (
	"context"
	"time"

	v1 "github.com/submariner-io/submariner/pkg/apis/submariner.io/v1"
	scheme "github.com/submariner-io/submariner/pkg/client/clientset/versioned/scheme"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// GlobalIngressIPsGetter has a method to return a GlobalIngressIPInterface.
// A group's client should implement this interface.
type GlobalIngressIPsGetter interface {
	GlobalIngressIPs(namespace string) GlobalIngressIPInterface
}

// GlobalIngressIPInterface has methods to work with GlobalIngressIP resources.
type GlobalIngressIPInterface interface {
	Create(ctx context.Context, globalIngressIP *v1.GlobalIngressIP, opts metav1.CreateOptions) (*v1.GlobalIngressIP, error)
	Update(ctx context.Context, globalIngressIP *v1.GlobalIngressIP, opts metav1.UpdateOptions) (*v1.GlobalIngressIP, error)
	UpdateStatus(ctx context.Context, globalIngressIP *v1.GlobalIngressIP, opts metav1.UpdateOptions) (*v1.GlobalIngressIP, error)
	Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*v1.GlobalIngressIP, error)
	List(ctx context.Context, opts metav1.ListOptions) (*v1.GlobalIngressIPList, error)
	Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *v1.GlobalIngressIP, err error)
	GlobalIngressIPExpansion
}

// globalIngressIPs implements GlobalIngressIPInterface
type globalIngressIPs struct {
	client rest.Interface
	ns     string
}

// newGlobalIngressIPs returns a GlobalIngressIPs
func newGlobalIngressIPs(c *SubmarinerV1Client, namespace string) *globalIngressIPs {
	return &globalIngressIPs{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the globalIngressIP, and returns the corresponding globalIngressIP object, and an error if there is any.
func (c *globalIngressIPs) Get(ctx context.Context, name string, options metav1.GetOptions) (result *v1.GlobalIngressIP, err error) {
	result = &v1.GlobalIngressIP{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("globalingressips").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do(ctx).
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of GlobalIngressIPs that match those selectors.
func (c *globalIngressIPs) List(ctx context.Context, opts metav1.ListOptions) (result *v1.GlobalIngressIPList, err error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	result = &v1.GlobalIngressIPList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("globalingressips").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Do(ctx).
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested globalIngressIPs.
func (c *globalIngressIPs) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("globalingressips").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Watch(ctx)
}

// Create takes the representation of a globalIngressIP and creates it.  Returns the server's representation of the globalIngressIP, and an error, if there is any.
func (c *globalIngressIPs) Create(ctx context.Context, globalIngressIP *v1.GlobalIngressIP, opts metav1.CreateOptions) (result *v1.GlobalIngressIP, err error) {
	result = &v1.GlobalIngressIP{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("globalingressips").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(globalIngressIP).
		Do(ctx).
		Into(result)
	return
}

// Update takes the representation of a globalIngressIP and updates it. Returns the server's representation of the globalIngressIP, and an error, if there is any.
func (c *globalIngressIPs) Update(ctx context.Context, globalIngressIP *v1.GlobalIngressIP, opts metav1.UpdateOptions) (result *v1.GlobalIngressIP, err error) {
	result = &v1.GlobalIngressIP{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("globalingressips").
		Name(globalIngressIP.Name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(globalIngressIP).
		Do(ctx).
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *globalIngressIPs) UpdateStatus(ctx context.Context, globalIngressIP *v1.GlobalIngressIP, opts metav1.UpdateOptions) (result *v1.GlobalIngressIP, err error) {
	result = &v1.GlobalIngressIP{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("globalingressips").
		Name(globalIngressIP.Name).
		SubResource("status").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(globalIngressIP).
		Do(ctx).
		Into(result)
	return
}

// Delete takes name of the globalIngressIP and deletes it. Returns an error if one occurs.
func (c *globalIngressIPs) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("globalingressips").
		Name(name).
		Body(&opts).
		Do(ctx).
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *globalIngressIPs) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	var timeout time.Duration
	if listOpts.TimeoutSeconds != nil {
		timeout = time.Duration(*listOpts.TimeoutSeconds) * time.Second
	}
	return c.client.Delete().
		Namespace(c.ns).
		Resource("globalingressips").
		VersionedParams(&listOpts, scheme.ParameterCodec).
		Timeout(timeout).
		Body(&opts).
		Do(ctx).
		Error()
}

// Patch applies the patch and returns the patched globalIngressIP.
func (c *globalIngressIPs) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *v1.GlobalIngressIP, err error) {
	result = &v1.GlobalIngressIP{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("globalingressips").
		Name(name).
		SubResource(subresources...).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(data).
		Do(ctx).
		Into(result)
	return
}
