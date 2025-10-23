package builder

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

type IDeploymentBuilder interface {
	WithAnnotations(annotations map[string]string)
	WithLabels(labels map[string]string)
	WithTemplateLabels(labels map[string]string)
	WithSelectorLabels(labels map[string]string)
	WithReplicas(replicas int32)
	WithContainerImage(containerImage string)
	WithContainerName(containerName string)
	WithEnvVars(envVars map[string]string)
	WithLivenessProbe(probe corev1.Probe)
	WithReadinessProbe(probe corev1.Probe)
	WithPorts(ports []corev1.ContainerPort)
	WithService(service *infrastructurev1alpha1.Service)
	Build() (appsv1.Deployment, string, error)
}

type DeploymentBuilder struct {
	deployment appsv1.Deployment
}

func (b *DeploymentBuilder) WithAnnotations(annotations map[string]string) {
	b.deployment.SetAnnotations(annotations)
}

func (b *DeploymentBuilder) WithLabels(labels map[string]string) {
	b.deployment.SetLabels(labels)
}

func (b *DeploymentBuilder) WithTemplateLabels(labels map[string]string) {
	if b.deployment.Spec.Template.Labels == nil {
		b.deployment.Spec.Template.Labels = make(map[string]string)
	}
	for key, value := range labels {
		b.deployment.Spec.Template.Labels[key] = value
	}
}

func (b *DeploymentBuilder) WithReplicas(replicas int32) {
	b.deployment.Spec.Replicas = &replicas
}

func (b *DeploymentBuilder) WithContainerImage(containerImage string) {
	if len(b.deployment.Spec.Template.Spec.Containers) == 0 {
		b.deployment.Spec.Template.Spec.Containers = append(b.deployment.Spec.Template.Spec.Containers, corev1.Container{})
	}
	b.deployment.Spec.Template.Spec.Containers[0].Image = containerImage
}

func (b *DeploymentBuilder) WithEnvVars(envVars map[string]string) {
	if len(b.deployment.Spec.Template.Spec.Containers) == 0 {
		b.deployment.Spec.Template.Spec.Containers = append(b.deployment.Spec.Template.Spec.Containers, corev1.Container{})
	}
	container := &b.deployment.Spec.Template.Spec.Containers[0]
	for key, value := range envVars {
		container.Env = append(container.Env, corev1.EnvVar{
			Name:  key,
			Value: value,
		})
	}
}

func (b *DeploymentBuilder) Build() (appsv1.Deployment, string, error) {
	marshalled, err := json.Marshal(b.deployment)
	if err != nil {
		return appsv1.Deployment{}, "", err
	}

	hash := fmt.Sprintf("%x", sha256.Sum256(marshalled))

	b.deployment.SetAnnotations(map[string]string{
		ValuesHashAnnotation: hash,
	})

	logger := logf.Log.WithName("deployment-builder")
	logger.V(1).Info("Built Deployment", "name", b.deployment.Name, "namespace", b.deployment.Namespace, "hash", hash, "marshal", string(marshalled))

	return b.deployment, hash, nil
}

func (b *DeploymentBuilder) WithLivenessProbe(probe corev1.Probe) {
	if len(b.deployment.Spec.Template.Spec.Containers) == 0 {
		return
	}
	b.deployment.Spec.Template.Spec.Containers[0].LivenessProbe = &probe
}

func (b *DeploymentBuilder) WithReadinessProbe(probe corev1.Probe) {
	if len(b.deployment.Spec.Template.Spec.Containers) == 0 {
		return
	}
	b.deployment.Spec.Template.Spec.Containers[0].ReadinessProbe = &probe
}

func (b *DeploymentBuilder) WithPorts(ports []corev1.ContainerPort) {
	if len(b.deployment.Spec.Template.Spec.Containers) == 0 {
		return
	}
	b.deployment.Spec.Template.Spec.Containers[0].Ports = ports
}

func (b *DeploymentBuilder) WithContainerName(containerName string) {
	if len(b.deployment.Spec.Template.Spec.Containers) == 0 {
		return
	}
	b.deployment.Spec.Template.Spec.Containers[0].Name = containerName
}

func (b *DeploymentBuilder) WithService(service *infrastructurev1alpha1.Service) {
	if service.Spec.OriginType == infrastructurev1alpha1.OriginTypeS3 && len(service.Spec.S3OriginSpec) > 0 {
		b.withS3GatewayCredentials(service.Spec.S3OriginSpec[0])
		b.withS3GatewayDomainName(service.Spec.Domain)
		b.WithContainerName("s3gateway")
	}
}

func (b *DeploymentBuilder) withS3GatewayCredentials(originSpec infrastructurev1alpha1.S3OriginSpec) {
	if len(b.deployment.Spec.Template.Spec.Containers) == 0 {
		return
	}

	b.deployment.Spec.Template.Spec.Containers[0].Env = []v1.EnvVar{
		{
			Name:  "AWS_ACCESS_KEY_ID",
			Value: originSpec.S3AccessKeyId,
		},
		{
			Name:  "AWS_SECRET_ACCESS_KEY",
			Value: originSpec.S3SecretKey,
		},
		{
			Name:  "S3_BUCKET_NAME",
			Value: originSpec.S3BucketName,
		},
		{
			Name:  "S3_REGION",
			Value: originSpec.S3Region,
		},
		{
			Name:  "S3_SERVER",
			Value: originSpec.S3Server,
		},
		{
			Name:  "S3_SERVER_PROTO",
			Value: originSpec.S3ServerProto,
		},
		{
			Name:  "S3_SERVER_PORT",
			Value: fmt.Sprintf("%d", originSpec.S3ServerPort),
		},
		{
			Name:  "S3_STYLE",
			Value: originSpec.S3Style,
		},
		{
			Name:  "AWS_SIGS_VERSION",
			Value: fmt.Sprintf("%d", originSpec.AwsSigsVersion),
		},
		{
			Name:  "ALLOW_DIRECTORY_LIST",
			Value: "false",
		},
	}
}

func (b *DeploymentBuilder) withS3GatewayDomainName(domain string) {
	b.WithTemplateLabels(map[string]string{
		"app":    "s3gateway",
		"domain": domain,
	})
	b.WithSelectorLabels(map[string]string{
		"app":    "s3gateway",
		"domain": domain,
	})
}

func (b *DeploymentBuilder) WithSelectorLabels(labels map[string]string) {
	b.deployment.Spec.Selector.MatchLabels = labels
}

func NewDeploymentBuilder(name string, namespace string) *DeploymentBuilder {
	return &DeploymentBuilder{
		deployment: appsv1.Deployment{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": name,
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app": name,
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: name,
							},
						},
					},
				},
			},
		},
	}
}

func DeploymentBuilderFactory(builderType string, name string, namespace string) (IDeploymentBuilder, error) {
	switch builderType {
	case "default":
		b := NewDeploymentBuilder(name, namespace)
		return b, nil
	case "s3-gateway":
		b := NewDeploymentBuilder(name, namespace)
		b.WithReplicas(1)
		b.WithContainerImage("fr6nco/nginx-s3-gateway:latest")
		b.WithLivenessProbe(v1.Probe{
			InitialDelaySeconds: 3,
			ProbeHandler: v1.ProbeHandler{
				HTTPGet: &v1.HTTPGetAction{
					Path: "/health",
					Port: intstr.FromInt(80),
				},
			},
		})
		b.WithReadinessProbe(v1.Probe{
			InitialDelaySeconds: 3,
			ProbeHandler: v1.ProbeHandler{
				HTTPGet: &v1.HTTPGetAction{
					Path: "/health",
					Port: intstr.FromInt(80),
				},
			},
		})
		b.WithPorts([]corev1.ContainerPort{
			{
				Name:          "http",
				ContainerPort: 80,
				Protocol:      corev1.ProtocolTCP,
			},
		})
		return b, nil
	default:
		return nil, fmt.Errorf("unknown builder type: %s", builderType)
	}
}
