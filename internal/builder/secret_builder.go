package builder

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

type ISecretBuilder interface {
	WithAnnotations(annotations map[string]string)
	WithLabels(labels map[string]string)
	WithData(data map[string][]byte)
	WithDataEntry(key string, value []byte)
	Build() (corev1.Secret, string, error)
}

type SecretBuilder struct {
	secret corev1.Secret
}

func (b *SecretBuilder) WithAnnotations(annotations map[string]string) {
	b.secret.SetAnnotations(annotations)
}

func (b *SecretBuilder) WithLabels(labels map[string]string) {
	b.secret.SetLabels(labels)
}

func (b *SecretBuilder) WithData(data map[string][]byte) {
	b.secret.Data = data
}

func (b *SecretBuilder) WithDataEntry(key string, value []byte) {
	if b.secret.Data == nil {
		b.secret.Data = make(map[string][]byte)
	}
	b.secret.Data[key] = value
}

func (b *SecretBuilder) Build() (corev1.Secret, string, error) {
	marhaslled, err := json.Marshal(b.secret)

	if err != nil {
		return corev1.Secret{}, "", err
	}

	hash := fmt.Sprintf("%x", sha256.Sum256(marhaslled))

	b.secret.SetAnnotations(map[string]string{
		ValuesHashAnnotation: hash,
	})

	logger := logf.Log.WithName("secret-builder")
	logger.V(1).Info("built secret", "hash", hash, "secret", b.secret)

	return b.secret, hash, nil
}

func NewSecretBuilder(name, namespace string) *SecretBuilder {
	return &SecretBuilder{
		secret: corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Secret",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Data: make(map[string][]byte),
		},
	}
}
