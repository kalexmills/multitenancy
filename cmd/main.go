package main

import (
	"context"
	"github.com/kalexmills/multitenancy/internal/controllers"
	apiv1alpha1 "github.com/kalexmills/multitenancy/pkg/apis/specs.kalexmills.com/v1alpha1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"log/slog"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func main() {
	ctx := context.Background()

	slog.SetLogLoggerLevel(slog.LevelInfo) // TODO: replace w/ zap

	cfg, err := rest.InClusterConfig()
	if err != nil {
		slog.Error("Could not fetch in cluster config: %v", err)
	}
	err = apiv1alpha1.Install(scheme.Scheme)
	if err != nil {
		slog.Error("Could not install scheme: %v", err)
	}
	c, err := client.NewWithWatch(cfg, client.Options{
		Scheme: scheme.Scheme,
	})
	if err != nil {
		slog.Error("Could not create client: %v", err)
	}

	c := controllers.NewTenantController(ctx, c)
}
