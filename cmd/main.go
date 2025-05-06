package main

import (
	"context"
	"github.com/kalexmills/multitenancy/internal/controllers"
	apiv1alpha1 "github.com/kalexmills/multitenancy/pkg/apis/specs.kalexmills.com/v1alpha1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"log/slog"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func main() {
	ctx := context.Background()

	slog.SetLogLoggerLevel(slog.LevelInfo) // TODO: replace w/ zap

	cfg, err := rest.InClusterConfig()
	if err != nil {
		slog.Error("Could not fetch in cluster config", "error", err)
		os.Exit(1)
	}

	err = apiv1alpha1.Install(scheme.Scheme)
	if err != nil {
		slog.Error("Could not install scheme", "error", err)
		os.Exit(1)
	}

	watchClient, err := client.NewWithWatch(cfg, client.Options{
		Scheme: scheme.Scheme,
	})
	if err != nil {
		slog.Error("Could not create controller-runtime client", "error", err)
		os.Exit(1)
	}

	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		slog.Error("Could not create dynamic k8s client", "error", err)
		os.Exit(1)
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		slog.Error("Could not create dynamic k8s discovery client", "error", err)
		os.Exit(1)
	}

	// TODO: setup OS signal handling
	_ = controllers.NewTenantController(ctx, watchClient, dynamicClient, discoveryClient)

	slog.Info("running controller")
	<-ctx.Done()
}
