/*
Copyright 2025 isola.

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

package ca

import (
	"context"
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// CASecretName is the name of the Secret containing the CA certificate and key
	CASecretName = "isola-sandbox-ca"
	// CABundleConfigMapName is the name of the ConfigMap containing CA certificates for verification
	CABundleConfigMapName = "isola-sandbox-ca-bundle"
)

// Manager handles CA lifecycle including initialization, persistence, and certificate issuance.
type Manager struct {
	client    client.Client
	namespace string
	ca        *CA
	mu        sync.RWMutex
}

// NewManager creates a new CA manager.
func NewManager(c client.Client, namespace string) *Manager {
	return &Manager{
		client:    c,
		namespace: namespace,
	}
}

// Initialize loads an existing CA from the cluster or creates a new one.
// This should be called during operator startup.
func (m *Manager) Initialize(ctx context.Context) error {
	log := logf.FromContext(ctx).WithName("ca-manager")

	secret := &corev1.Secret{}
	err := m.client.Get(ctx, client.ObjectKey{Name: CASecretName, Namespace: m.namespace}, secret)

	if apierrors.IsNotFound(err) {
		log.Info("CA secret not found, generating new CA")
		return m.createCA(ctx)
	}
	if err != nil {
		return fmt.Errorf("getting CA secret: %w", err)
	}

	ca, err := Load(secret.Data["ca.crt"], secret.Data["ca.key"])
	if err != nil {
		return fmt.Errorf("loading CA from secret: %w", err)
	}

	m.mu.Lock()
	m.ca = ca
	m.mu.Unlock()

	log.Info("Loaded existing CA from secret")

	// Ensure ConfigMap exists and is up to date
	if err := m.ensureCABundleConfigMap(ctx); err != nil {
		return fmt.Errorf("ensuring CA bundle ConfigMap: %w", err)
	}

	return nil
}

func (m *Manager) createCA(ctx context.Context) error {
	log := logf.FromContext(ctx).WithName("ca-manager")

	ca, err := Generate()
	if err != nil {
		return fmt.Errorf("generating CA: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      CASecretName,
			Namespace: m.namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "isola-sandbox-ca",
				"app.kubernetes.io/component":  "security",
				"app.kubernetes.io/managed-by": "isola-operator",
			},
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": ca.CertPEM(),
			"tls.key": ca.KeyPEM(),
			"ca.crt":  ca.CertPEM(),
			"ca.key":  ca.KeyPEM(),
		},
	}

	if err := m.client.Create(ctx, secret); err != nil {
		if apierrors.IsAlreadyExists(err) {
			// Race condition - another instance created it, reload
			return m.Initialize(ctx)
		}
		return fmt.Errorf("creating CA secret: %w", err)
	}

	m.mu.Lock()
	m.ca = ca
	m.mu.Unlock()

	log.Info("Created new CA secret")

	// Create ConfigMap with CA public cert for isola-gw
	if err := m.ensureCABundleConfigMap(ctx); err != nil {
		return fmt.Errorf("creating CA bundle ConfigMap: %w", err)
	}

	return nil
}

func (m *Manager) ensureCABundleConfigMap(ctx context.Context) error {
	log := logf.FromContext(ctx).WithName("ca-manager")

	m.mu.RLock()
	caCertPEM := string(m.ca.CertPEM())
	m.mu.RUnlock()

	cm := &corev1.ConfigMap{}
	err := m.client.Get(ctx, client.ObjectKey{Name: CABundleConfigMapName, Namespace: m.namespace}, cm)

	if apierrors.IsNotFound(err) {
		cm = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      CABundleConfigMapName,
				Namespace: m.namespace,
				Labels: map[string]string{
					"app.kubernetes.io/name":       "isola-sandbox-ca-bundle",
					"app.kubernetes.io/component":  "security",
					"app.kubernetes.io/managed-by": "isola-operator",
				},
			},
			Data: map[string]string{
				"ca-bundle.crt": caCertPEM,
			},
		}
		if err := m.client.Create(ctx, cm); err != nil {
			if apierrors.IsAlreadyExists(err) {
				return nil
			}
			return fmt.Errorf("creating CA bundle ConfigMap: %w", err)
		}
		log.Info("Created CA bundle ConfigMap")
		return nil
	}
	if err != nil {
		return fmt.Errorf("getting CA bundle ConfigMap: %w", err)
	}

	// Update if needed (e.g., after CA rotation)
	if cm.Data["ca-bundle.crt"] != caCertPEM {
		// Append new CA to bundle (keeps old CA for transition period)
		existingBundle := cm.Data["ca-bundle.crt"]
		if existingBundle != "" && existingBundle != caCertPEM {
			cm.Data["ca-bundle.crt"] = caCertPEM + existingBundle
		} else {
			cm.Data["ca-bundle.crt"] = caCertPEM
		}
		if err := m.client.Update(ctx, cm); err != nil {
			return fmt.Errorf("updating CA bundle ConfigMap: %w", err)
		}
		log.Info("Updated CA bundle ConfigMap")
	}

	return nil
}

// IssueCert generates a certificate for a sandbox agent.
// The sandbox UUID is embedded in the certificate for verification by isola-gw.
func (m *Manager) IssueCert(sandboxUUID, podDNS string) (certPEM, keyPEM []byte, err error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.ca == nil {
		return nil, nil, fmt.Errorf("CA not initialized")
	}

	return m.ca.IssueCert(sandboxUUID, podDNS)
}

// CACertPEM returns the CA certificate in PEM format.
func (m *Manager) CACertPEM() []byte {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.ca == nil {
		return nil
	}
	return m.ca.CertPEM()
}

// Namespace returns the namespace where CA resources are stored.
func (m *Manager) Namespace() string {
	return m.namespace
}
