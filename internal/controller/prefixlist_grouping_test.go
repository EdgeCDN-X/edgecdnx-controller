/*
Copyright 2025.

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
	"strings"
	"testing"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func TestGeneratedPrefixListName(t *testing.T) {
	t.Run("preserves the legacy name without labels", func(t *testing.T) {
		for _, labels := range []map[string]string{nil, {}} {
			if got := generatedPrefixListName("location-a", labels); got != "location-a-generated" {
				t.Fatalf("generatedPrefixListName() = %q, want %q", got, "location-a-generated")
			}
		}
	})

	t.Run("is stable for the same labels", func(t *testing.T) {
		first := generatedPrefixListName("location-a", map[string]string{"region": "eu", "tier": "edge"})
		second := generatedPrefixListName("location-a", map[string]string{"tier": "edge", "region": "eu"})
		if first != second {
			t.Fatalf("same labels produced different names: %q and %q", first, second)
		}
	})

	t.Run("separates different labels", func(t *testing.T) {
		first := generatedPrefixListName("location-a", map[string]string{"tier": "edge"})
		second := generatedPrefixListName("location-a", map[string]string{"tier": "origin"})
		if first == second {
			t.Fatalf("different labels produced the same name: %q", first)
		}
		if hash := strings.TrimPrefix(first, "location-a-generated-"); len(hash) != 16 {
			t.Fatalf("generated hash has %d characters, want 16", len(hash))
		}
	})

	t.Run("stays within the Kubernetes name limit", func(t *testing.T) {
		first := generatedPrefixListName(strings.Repeat("a", validation.DNS1123SubdomainMaxLength), map[string]string{"tier": "edge"})
		second := generatedPrefixListName(strings.Repeat("a", validation.DNS1123SubdomainMaxLength-1)+"b", map[string]string{"tier": "edge"})

		for _, name := range []string{first, second} {
			if len(name) > validation.DNS1123SubdomainMaxLength {
				t.Fatalf("generated name has %d characters, maximum is %d", len(name), validation.DNS1123SubdomainMaxLength)
			}
			if errors := validation.IsDNS1123Subdomain(name); len(errors) > 0 {
				t.Fatalf("generated name %q is invalid: %v", name, errors)
			}
		}
		if first == second {
			t.Fatalf("different long destinations produced the same name: %q", first)
		}
	})
}

func TestRemoveFromOtherGeneratedPrefixLists(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := infrastructurev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}

	firstSource := &infrastructurev1alpha1.PrefixList{
		TypeMeta: metav1.TypeMeta{APIVersion: infrastructurev1alpha1.SchemeGroupVersion.String(), Kind: "PrefixList"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "first-source",
			Namespace: "default",
			UID:       types.UID("first-source-uid"),
		},
	}
	secondSource := &infrastructurev1alpha1.PrefixList{
		TypeMeta: metav1.TypeMeta{APIVersion: infrastructurev1alpha1.SchemeGroupVersion.String(), Kind: "PrefixList"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "second-source",
			Namespace: "default",
			UID:       types.UID("second-source-uid"),
		},
	}
	legacyGenerated := &infrastructurev1alpha1.PrefixList{
		ObjectMeta: metav1.ObjectMeta{Name: "location-a-generated", Namespace: "default"},
		Spec:       infrastructurev1alpha1.PrefixListSpec{Source: SourceController},
	}
	for _, source := range []*infrastructurev1alpha1.PrefixList{firstSource, secondSource} {
		if err := controllerutil.SetOwnerReference(source, legacyGenerated, scheme); err != nil {
			t.Fatalf("SetOwnerReference() error = %v", err)
		}
	}

	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(firstSource, secondSource, legacyGenerated).Build()
	reconciler := &PrefixListReconciler{Client: client, Scheme: scheme}
	ctx := context.Background()

	if err := reconciler.removeFromOtherGeneratedPrefixLists(ctx, firstSource, "location-a-generated-first"); err != nil {
		t.Fatalf("removeFromOtherGeneratedPrefixLists() error = %v", err)
	}
	remaining := &infrastructurev1alpha1.PrefixList{}
	key := types.NamespacedName{Name: legacyGenerated.Name, Namespace: legacyGenerated.Namespace}
	if err := client.Get(ctx, key, remaining); err != nil {
		t.Fatalf("legacy generated PrefixList was removed while it still had an owner: %v", err)
	}
	if len(remaining.OwnerReferences) != 1 || remaining.OwnerReferences[0].UID != secondSource.UID {
		t.Fatalf("legacy generated PrefixList owners = %#v, want only %q", remaining.OwnerReferences, secondSource.UID)
	}

	if err := reconciler.removeFromOtherGeneratedPrefixLists(ctx, secondSource, "location-a-generated-second"); err != nil {
		t.Fatalf("removeFromOtherGeneratedPrefixLists() error = %v", err)
	}
	if err := client.Get(ctx, key, remaining); !apierrors.IsNotFound(err) {
		t.Fatalf("legacy generated PrefixList still exists after its last owner migrated: %v", err)
	}
}
