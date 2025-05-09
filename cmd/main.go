package main

import (
	"context"
	"github.com/kalexmills/multitenancy/internal/controllers"
	apiv1alpha1 "github.com/kalexmills/multitenancy/pkg/apis/specs.kalexmills.com/v1alpha1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"log/slog"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func main() {
	ctx := context.Background()

	slog.SetLogLoggerLevel(slog.LevelInfo)
	l := slog.With("component", "setup")

	cfg, err := rest.InClusterConfig()
	if err != nil {
		l.Error("Could not fetch in cluster config", "error", err)
		os.Exit(1)
	}

	err = apiv1alpha1.Install(scheme.Scheme)
	if err != nil {
		l.Error("Could not install scheme", "error", err)
		os.Exit(1)
	}

	watchClient, err := client.NewWithWatch(cfg, client.Options{
		Scheme: scheme.Scheme,
	})
	if err != nil {
		l.Error("Could not create controller-runtime client", "error", err)
		os.Exit(1)
	}

	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		l.Error("Could not create dynamic k8s client", "error", err)
		os.Exit(1)
	}

	_ = controllers.NewManager(ctx, watchClient, dynamicClient)

	l.Info("running controller")
	<-ctx.Done()
}
