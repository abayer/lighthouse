// Code generated by client-gen. DO NOT EDIT.

package v1alpha1

import (
	v1alpha1 "github.com/jenkins-x/lighthouse/pkg/apis/lighthouse/v1alpha1"
	scheme "github.com/jenkins-x/lighthouse/pkg/client/clientset/versioned/scheme"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// LighthouseJobsGetter has a method to return a LighthouseJobInterface.
// A group's client should implement this interface.
type LighthouseJobsGetter interface {
	LighthouseJobs(namespace string) LighthouseJobInterface
}

// LighthouseJobInterface has methods to work with LighthouseJob resources.
type LighthouseJobInterface interface {
	Create(*v1alpha1.LighthouseJob) (*v1alpha1.LighthouseJob, error)
	Update(*v1alpha1.LighthouseJob) (*v1alpha1.LighthouseJob, error)
	UpdateStatus(*v1alpha1.LighthouseJob) (*v1alpha1.LighthouseJob, error)
	Delete(name string, options *v1.DeleteOptions) error
	DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error
	Get(name string, options v1.GetOptions) (*v1alpha1.LighthouseJob, error)
	List(opts v1.ListOptions) (*v1alpha1.LighthouseJobList, error)
	Watch(opts v1.ListOptions) (watch.Interface, error)
	Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha1.LighthouseJob, err error)
	LighthouseJobExpansion
}

// lighthouseJobs implements LighthouseJobInterface
type lighthouseJobs struct {
	client rest.Interface
	ns     string
}

// newLighthouseJobs returns a LighthouseJobs
func newLighthouseJobs(c *LighthouseV1alpha1Client, namespace string) *lighthouseJobs {
	return &lighthouseJobs{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the lighthouseJob, and returns the corresponding lighthouseJob object, and an error if there is any.
func (c *lighthouseJobs) Get(name string, options v1.GetOptions) (result *v1alpha1.LighthouseJob, err error) {
	result = &v1alpha1.LighthouseJob{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("lighthousejobs").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of LighthouseJobs that match those selectors.
func (c *lighthouseJobs) List(opts v1.ListOptions) (result *v1alpha1.LighthouseJobList, err error) {
	result = &v1alpha1.LighthouseJobList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("lighthousejobs").
		VersionedParams(&opts, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested lighthouseJobs.
func (c *lighthouseJobs) Watch(opts v1.ListOptions) (watch.Interface, error) {
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("lighthousejobs").
		VersionedParams(&opts, scheme.ParameterCodec).
		Watch()
}

// Create takes the representation of a lighthouseJob and creates it.  Returns the server's representation of the lighthouseJob, and an error, if there is any.
func (c *lighthouseJobs) Create(lighthouseJob *v1alpha1.LighthouseJob) (result *v1alpha1.LighthouseJob, err error) {
	result = &v1alpha1.LighthouseJob{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("lighthousejobs").
		Body(lighthouseJob).
		Do().
		Into(result)
	return
}

// Update takes the representation of a lighthouseJob and updates it. Returns the server's representation of the lighthouseJob, and an error, if there is any.
func (c *lighthouseJobs) Update(lighthouseJob *v1alpha1.LighthouseJob) (result *v1alpha1.LighthouseJob, err error) {
	result = &v1alpha1.LighthouseJob{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("lighthousejobs").
		Name(lighthouseJob.Name).
		Body(lighthouseJob).
		Do().
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().

func (c *lighthouseJobs) UpdateStatus(lighthouseJob *v1alpha1.LighthouseJob) (result *v1alpha1.LighthouseJob, err error) {
	result = &v1alpha1.LighthouseJob{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("lighthousejobs").
		Name(lighthouseJob.Name).
		SubResource("status").
		Body(lighthouseJob).
		Do().
		Into(result)
	return
}

// Delete takes name of the lighthouseJob and deletes it. Returns an error if one occurs.
func (c *lighthouseJobs) Delete(name string, options *v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("lighthousejobs").
		Name(name).
		Body(options).
		Do().
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *lighthouseJobs) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("lighthousejobs").
		VersionedParams(&listOptions, scheme.ParameterCodec).
		Body(options).
		Do().
		Error()
}

// Patch applies the patch and returns the patched lighthouseJob.
func (c *lighthouseJobs) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha1.LighthouseJob, err error) {
	result = &v1alpha1.LighthouseJob{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("lighthousejobs").
		SubResource(subresources...).
		Name(name).
		Body(data).
		Do().
		Into(result)
	return
}
