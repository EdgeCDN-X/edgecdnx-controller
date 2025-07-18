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

package main

import (
	"crypto/tls"
	"flag"
	"os"
	"path/filepath"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	argoprojv1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	"github.com/EdgeCDN-X/edgecdnx-controller/internal/controller"
	acmev1 "github.com/cert-manager/cert-manager/pkg/apis/acme/v1"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	v1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(infrastructurev1alpha1.AddToScheme(scheme))
	utilruntime.Must(argoprojv1alpha1.AddToScheme(scheme))
	utilruntime.Must(networkingv1.AddToScheme(scheme))
	utilruntime.Must(v1.AddToScheme(scheme))
	utilruntime.Must(certmanagerv1.AddToScheme(scheme))
	utilruntime.Must(acmev1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

// nolint:gocyclo
func main() {
	var metricsAddr string
	var metricsCertPath, metricsCertName, metricsCertKey string
	var webhookCertPath, webhookCertName, webhookCertKey string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool

	// Common
	var throwerChartRepository string
	var throwerChartName string
	var throwerChartVersion string

	// Infrastructure
	var infrastructureApplicationSetProject string
	var infrastructureTargetNamespace string
	var clusterIssuerName string

	// Role
	var role string

	// Consul Endpoint - no TLS or auth support for now
	var consulEndpoint string

	// Secure URLs
	var secureUrlsEndpoint string

	var tlsOpts []func(*tls.Config)
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.StringVar(&webhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	flag.StringVar(&webhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	flag.StringVar(&webhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	flag.StringVar(&metricsCertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	flag.StringVar(&metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	flag.StringVar(&metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")

	flag.StringVar(&role, "role", "controller", "The role of this instance. Can be 'controller', runs on control plane, 'cache-controller', runs on edge nodes, 'router', runs on routing nodes")

	flag.StringVar(&consulEndpoint, "consul-endpoint", "http://edgecdnx-consul-consul-server:8500", "Default Consul Endpoint")

	flag.StringVar(
		&throwerChartRepository,
		"thrower-chart-repository",
		"https://edgecdn-x.github.io/helm-charts",
		"Repository URL for the helm chart",
	)
	flag.StringVar(
		&throwerChartName,
		"thrower-chart-name",
		"resource-thrower",
		"Name of the helm chart for the thrower",
	)
	flag.StringVar(
		&throwerChartVersion,
		"thrower-chart-version",
		"0.1.0",
		"Version of the helm chart for the thrower",
	)

	flag.StringVar(
		&infrastructureTargetNamespace,
		"infrastructure-target-namespace",
		"edgecdnx-routing",
		"The namespace where the infrastructure resources are deployed.",
	)
	flag.StringVar(
		&infrastructureApplicationSetProject,
		"infrastructure-application-set-project",
		"default",
		"The project of the infrastructure ApplicationSet.",
	)

	flag.StringVar(
		&clusterIssuerName,
		"cluster-issuer-name",
		"edgecdnx",
		"The name of the cluster issuer to use for certificate management.",
	)

	flag.StringVar(
		&secureUrlsEndpoint,
		"secure-urls-endpoint",
		"http://secure-urls.edgecdnx-cache.svc.cluster.local",
		"The endpoint for the secure URLs service.",
	)

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// Create watchers for metrics and webhooks certificates
	var metricsCertWatcher, webhookCertWatcher *certwatcher.CertWatcher

	// Initial webhook TLS options
	webhookTLSOpts := tlsOpts

	if len(webhookCertPath) > 0 {
		setupLog.Info("Initializing webhook certificate watcher using provided certificates",
			"webhook-cert-path", webhookCertPath, "webhook-cert-name", webhookCertName, "webhook-cert-key", webhookCertKey)

		var err error
		webhookCertWatcher, err = certwatcher.New(
			filepath.Join(webhookCertPath, webhookCertName),
			filepath.Join(webhookCertPath, webhookCertKey),
		)
		if err != nil {
			setupLog.Error(err, "Failed to initialize webhook certificate watcher")
			os.Exit(1)
		}

		webhookTLSOpts = append(webhookTLSOpts, func(config *tls.Config) {
			config.GetCertificate = webhookCertWatcher.GetCertificate
		})
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: webhookTLSOpts,
	})

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.4/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if secureMetrics {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.4/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// If the certificate is not specified, controller-runtime will automatically
	// generate self-signed certificates for the metrics server. While convenient for development and testing,
	// this setup is not recommended for production.
	//
	// TODO(user): If you enable certManager, uncomment the following lines:
	// - [METRICS-WITH-CERTS] at config/default/kustomization.yaml to generate and use certificates
	// managed by cert-manager for the metrics server.
	// - [PROMETHEUS-WITH-CERTS] at config/prometheus/kustomization.yaml for TLS certification.
	if len(metricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", metricsCertPath, "metrics-cert-name", metricsCertName, "metrics-cert-key", metricsCertKey)

		var err error
		metricsCertWatcher, err = certwatcher.New(
			filepath.Join(metricsCertPath, metricsCertName),
			filepath.Join(metricsCertPath, metricsCertKey),
		)
		if err != nil {
			setupLog.Error(err, "to initialize metrics certificate watcher", "error", err)
			os.Exit(1)
		}

		metricsServerOptions.TLSOpts = append(metricsServerOptions.TLSOpts, func(config *tls.Config) {
			config.GetCertificate = metricsCertWatcher.GetCertificate
		})
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "9cbc19dd.edgecdnx.com",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if role == "controller" {
		if err = (&controller.LocationReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
			ThrowerOptions: controller.ThrowerOptions{
				ThrowerChartName:                    throwerChartName,
				ThrowerChartVersion:                 throwerChartVersion,
				ThrowerChartRepository:              throwerChartRepository,
				InfrastructureTargetNamespace:       infrastructureTargetNamespace,
				InfrastructureApplicationSetProject: infrastructureApplicationSetProject,
			},
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "Location")
			os.Exit(1)
		}
	}

	if role == "controller" {
		if err = (&controller.ServiceReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
			ThrowerOptions: controller.ThrowerOptions{
				ThrowerChartName:                    throwerChartName,
				ThrowerChartVersion:                 throwerChartVersion,
				ThrowerChartRepository:              throwerChartRepository,
				InfrastructureTargetNamespace:       infrastructureTargetNamespace,
				InfrastructureApplicationSetProject: infrastructureApplicationSetProject,
			},
			ClusterIssuerName: clusterIssuerName,
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "Service")
			os.Exit(1)
		}
	}

	if role == "controller" {
		if err = (&controller.PrefixListReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
			ThrowerOptions: controller.ThrowerOptions{
				ThrowerChartName:                    throwerChartName,
				ThrowerChartVersion:                 throwerChartVersion,
				ThrowerChartRepository:              throwerChartRepository,
				InfrastructureTargetNamespace:       infrastructureTargetNamespace,
				InfrastructureApplicationSetProject: infrastructureApplicationSetProject,
			},
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "PrefixList")
			os.Exit(1)
		}
	}

	if role == "controller" {
		if err = (&controller.ChallengeReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
			ThrowerOptions: controller.ThrowerOptions{
				ThrowerChartName:                    throwerChartName,
				ThrowerChartVersion:                 throwerChartVersion,
				ThrowerChartRepository:              throwerChartRepository,
				InfrastructureTargetNamespace:       infrastructureTargetNamespace,
				InfrastructureApplicationSetProject: infrastructureApplicationSetProject,
			},
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "Challenge")
			os.Exit(1)
		}
	}

	if role == "cache-controller" {
		if err = (&controller.ServiceCacheReconciler{
			Client:             mgr.GetClient(),
			Scheme:             mgr.GetScheme(),
			SecureUrlsEndpoint: secureUrlsEndpoint,
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "ServiceCache")
			os.Exit(1)
		}
	}

	if role == "router" {
		if err = (&controller.LocationRoutingReconciler{
			Client:         mgr.GetClient(),
			Scheme:         mgr.GetScheme(),
			ConsulEndpoint: consulEndpoint,
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "LocationRouting")
			os.Exit(1)
		}
	}

	// +kubebuilder:scaffold:builder

	if metricsCertWatcher != nil {
		setupLog.Info("Adding metrics certificate watcher to manager")
		if err := mgr.Add(metricsCertWatcher); err != nil {
			setupLog.Error(err, "unable to add metrics certificate watcher to manager")
			os.Exit(1)
		}
	}

	if webhookCertWatcher != nil {
		setupLog.Info("Adding webhook certificate watcher to manager")
		if err := mgr.Add(webhookCertWatcher); err != nil {
			setupLog.Error(err, "unable to add webhook certificate watcher to manager")
			os.Exit(1)
		}
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
