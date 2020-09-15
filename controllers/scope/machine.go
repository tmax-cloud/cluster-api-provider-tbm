/*
Copyright 2018 The Kubernetes Authors.

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

package scope

import (
	"context"
	"encoding/base64"
	"fmt"

	infrav1 "cluster-api-provider-tbm/api/v1alpha3"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/klogr"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// MachineScopeParams defines the input parameters used to create a new MachineScope.
type MachineScopeParams struct {
	Client client.Client
	Logger logr.Logger

	Cluster      *clusterv1.Cluster
	Machine      *clusterv1.Machine
	InfraCluster *infrav1.TbmCluster
	TbmMachine   *infrav1.TbmMachine
}

// NewMachineScope creates a new MachineScope from the supplied parameters.
// This is meant to be called for each reconcile iteration.
func NewMachineScope(params MachineScopeParams) (*MachineScope, error) {
	if params.Client == nil {
		return nil, errors.New("client is required when creating a MachineScope")
	}
	if params.Machine == nil {
		return nil, errors.New("machine is required when creating a MachineScope")
	}
	if params.Cluster == nil {
		return nil, errors.New("cluster is required when creating a MachineScope")
	}
	if params.TbmMachine == nil {
		return nil, errors.New("tbm machine is required when creating a MachineScope")
	}
	if params.InfraCluster == nil {
		return nil, errors.New("tbm cluster is required when creating a MachineScope")
	}

	if params.Logger == nil {
		params.Logger = klogr.New()
	}

	helper, err := patch.NewHelper(params.TbmMachine, params.Client)
	if err != nil {
		return nil, errors.Wrap(err, "failed to init patch helper")
	}
	return &MachineScope{
		Logger:      params.Logger,
		client:      params.Client,
		patchHelper: helper,

		Cluster:      params.Cluster,
		Machine:      params.Machine,
		InfraCluster: params.InfraCluster,
		TbmMachine:   params.TbmMachine,
	}, nil
}

// MachineScope defines a scope defined around a machine and its cluster.
type MachineScope struct {
	logr.Logger
	client      client.Client
	patchHelper *patch.Helper

	Cluster      *clusterv1.Cluster
	Machine      *clusterv1.Machine
	InfraCluster *infrav1.TbmCluster
	TbmMachine   *infrav1.TbmMachine
}

// Name returns the TbmMachine name.
func (m *MachineScope) Name() string {
	return m.TbmMachine.Name
}

// Namespace returns the namespace name.
func (m *MachineScope) Namespace() string {
	return m.TbmMachine.Namespace
}

// IsControlPlane returns true if the machine is a control plane.
func (m *MachineScope) IsControlPlane() bool {
	return util.IsControlPlaneMachine(m.Machine)
}

// Role returns the machine role from the labels.
func (m *MachineScope) Role() string {
	if util.IsControlPlaneMachine(m.Machine) {
		return "control-plane"
	}
	return "node"
}

// GetProviderID returns the TbmMachine providerID from the spec.
func (m *MachineScope) GetProviderID() string {
	if m.TbmMachine.Spec.ProviderID != "" {
		return m.TbmMachine.Spec.ProviderID
	}
	return ""
}

// SetProviderID sets the TbmMachine providerID in spec.
func (m *MachineScope) SetProviderID(tbmName string) {
	providerID := fmt.Sprintf("tbm:///%s", tbmName)
	m.TbmMachine.Spec.ProviderID = providerID
}

// SetReady sets the AWSMachine Ready Status
func (m *MachineScope) SetReady() {
	m.TbmMachine.Status.Ready = true
}

// SetNotReady sets the AWSMachine Ready Status to false
func (m *MachineScope) SetNotReady() {
	m.TbmMachine.Status.Ready = false
}

// SetAnnotation sets a key value annotation on the AWSMachine.
func (m *MachineScope) SetAnnotation(key, value string) {
	if m.TbmMachine.Annotations == nil {
		m.TbmMachine.Annotations = map[string]string{}
	}
	m.TbmMachine.Annotations[key] = value
}

// SetAddresses sets the AWSMachine address status.
func (m *MachineScope) SetAddresses(addrs []clusterv1.MachineAddress) {
	m.TbmMachine.Status.Addresses = addrs
}

// GetBootstrapData returns the bootstrap data from the secret in the Machine's bootstrap.dataSecretName as base64.
func (m *MachineScope) GetBootstrapData() (string, error) {
	value, err := m.GetRawBootstrapData()
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(value), nil
}

// GetRawBootstrapData returns the bootstrap data from the secret in the Machine's bootstrap.dataSecretName.
func (m *MachineScope) GetRawBootstrapData() ([]byte, error) {
	if m.Machine.Spec.Bootstrap.DataSecretName == nil {
		return nil, errors.New("error retrieving bootstrap data: linked Machine's bootstrap.dataSecretName is nil")
	}

	secret := &corev1.Secret{}
	key := types.NamespacedName{Namespace: m.Namespace(), Name: *m.Machine.Spec.Bootstrap.DataSecretName}
	if err := m.client.Get(context.TODO(), key, secret); err != nil {
		return nil, errors.Wrapf(err, "failed to retrieve bootstrap data secret for AWSMachine %s/%s", m.Namespace(), m.Name())
	}

	value, ok := secret.Data["value"]
	if !ok {
		return nil, errors.New("error retrieving bootstrap data: secret value key is missing")
	}

	return value, nil
}

// PatchObject persists the machine spec and status.
func (m *MachineScope) PatchObject() error {
	return m.patchHelper.Patch(
		context.TODO(),
		m.TbmMachine)
}

// Close the MachineScope by updating the machine spec, machine status.
func (m *MachineScope) Close() error {
	return m.PatchObject()
}
