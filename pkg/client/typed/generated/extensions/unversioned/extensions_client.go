/*
Copyright 2016 The Kubernetes Authors All rights reserved.

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

package unversioned

import (
	"fmt"
	latest "k8s.io/kubernetes/pkg/api/latest"
	unversioned "k8s.io/kubernetes/pkg/client/unversioned"
)

type ExtensionsInterface interface {
	DaemonSetsGetter
	DeploymentsGetter
	HorizontalPodAutoscalersGetter
	IngressesGetter
	JobsGetter
	ScalesGetter
	ThirdPartyResourcesGetter
}

// ExtensionsClient is used to interact with features provided by the Extensions group.
type ExtensionsClient struct {
	*unversioned.RESTClient
}

func (c *ExtensionsClient) DaemonSets(namespace string) DaemonSetInterface {
	return newDaemonSets(c, namespace)
}

func (c *ExtensionsClient) Deployments(namespace string) DeploymentInterface {
	return newDeployments(c, namespace)
}

func (c *ExtensionsClient) HorizontalPodAutoscalers(namespace string) HorizontalPodAutoscalerInterface {
	return newHorizontalPodAutoscalers(c, namespace)
}

func (c *ExtensionsClient) Ingresses(namespace string) IngressInterface {
	return newIngresses(c, namespace)
}

func (c *ExtensionsClient) Jobs(namespace string) JobInterface {
	return newJobs(c, namespace)
}

func (c *ExtensionsClient) Scales(namespace string) ScaleInterface {
	return newScales(c, namespace)
}

func (c *ExtensionsClient) ThirdPartyResources(namespace string) ThirdPartyResourceInterface {
	return newThirdPartyResources(c, namespace)
}

// NewForConfig creates a new ExtensionsClient for the given config.
func NewForConfig(c *unversioned.Config) (*ExtensionsClient, error) {
	config := *c
	if err := setConfigDefaults(&config); err != nil {
		return nil, err
	}
	client, err := unversioned.RESTClientFor(&config)
	if err != nil {
		return nil, err
	}
	return &ExtensionsClient{client}, nil
}

// NewForConfigOrDie creates a new ExtensionsClient for the given config and
// panics if there is an error in the config.
func NewForConfigOrDie(c *unversioned.Config) *ExtensionsClient {
	client, err := NewForConfig(c)
	if err != nil {
		panic(err)
	}
	return client
}

// New creates a new ExtensionsClient for the given RESTClient.
func New(c *unversioned.RESTClient) *ExtensionsClient {
	return &ExtensionsClient{c}
}

func setConfigDefaults(config *unversioned.Config) error {
	// if extensions group is not registered, return an error
	g, err := latest.Group("extensions")
	if err != nil {
		return err
	}
	config.Prefix = "/apis"
	if config.UserAgent == "" {
		config.UserAgent = unversioned.DefaultKubernetesUserAgent()
	}
	// TODO: Unconditionally set the config.Version, until we fix the config.
	//if config.Version == "" {
	copyGroupVersion := g.GroupVersion
	config.GroupVersion = &copyGroupVersion
	//}

	versionInterfaces, err := g.InterfacesFor(*config.GroupVersion)
	if err != nil {
		return fmt.Errorf("Extensions API version '%s' is not recognized (valid values: %s)",
			config.GroupVersion, g.GroupVersions)
	}
	config.Codec = versionInterfaces.Codec
	if config.QPS == 0 {
		config.QPS = 5
	}
	if config.Burst == 0 {
		config.Burst = 10
	}
	return nil
}
