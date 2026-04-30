/*
Copyright 2026.

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

package controller

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// generateToken creates a 32-byte URL-safe random token.
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// ensureSecret returns the value at key in the secret name/ns.
// If the secret does not exist, it creates it with a new random token
// and an OwnerReference pointing to owner (auto-deleted on CR deletion).
func ensureSecret(ctx context.Context, c client.Client, scheme *runtime.Scheme,
	owner client.Object, ns, name, key string) (string, error) {

	var s corev1.Secret
	err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, &s)
	if errors.IsNotFound(err) {
		token, genErr := generateToken()
		if genErr != nil {
			return "", genErr
		}
		s = corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Data:       map[string][]byte{key: []byte(token)},
		}
		if setErr := controllerutil.SetControllerReference(owner, &s, scheme); setErr != nil {
			return "", fmt.Errorf("set owner reference on secret %s/%s: %w", ns, name, setErr)
		}
		if createErr := c.Create(ctx, &s); createErr != nil {
			return "", fmt.Errorf("create secret %s/%s: %w", ns, name, createErr)
		}
		return token, nil
	}
	if err != nil {
		return "", fmt.Errorf("get secret %s/%s: %w", ns, name, err)
	}
	val, ok := s.Data[key]
	if !ok {
		return "", fmt.Errorf("secret %s/%s has no key %q", ns, name, key)
	}
	return string(val), nil
}
