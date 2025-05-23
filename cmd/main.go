package main

import (
	"context"
	apiv1alpha1 "github.com/kalexmills/multitenancy/pkg/apis/specs.kalexmills.com/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
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

	//dynamicClient, err := dynamic.NewForConfig(cfg)
	//if err != nil {
	//	l.Error("Could not create dynamic k8s client", "error", err)
	//	os.Exit(1)
	//}

	go testPodWatch(ctx, l, watchClient)

	go testConfigWatch(ctx, l, watchClient)

	//
	//_ = controllers.NewManager(ctx, watchClient, dynamicClient)
	//

	l.Info("running controller")
	<-ctx.Done()
}

func testPodWatch(ctx context.Context, l *slog.Logger, wc client.WithWatch) {
	resourceVersion := os.Getenv("POD_RESOURCE_VERSION")

	wi, err := wc.Watch(ctx, &corev1.PodList{}, &client.ListOptions{
		Raw: &metav1.ListOptions{
			ResourceVersion:     resourceVersion,
			AllowWatchBookmarks: true,
			SendInitialEvents:   ptr.To(true),
		},
	})

	if err != nil {
		l.Error("error watching", "error", err)
		os.Exit(1)
	}

	l.Info("Pod watch successful", "resourceVersion", resourceVersion)

	for event := range wi.ResultChan() {
		pod := event.Object.(*corev1.Pod)
		l.Info("Pod event", "type", event.Type, "name", pod.Name, "namespace", pod.Namespace, "resourceVersion", pod.ResourceVersion)
	}
}

func testConfigWatch(ctx context.Context, l *slog.Logger, wc client.WithWatch) {
	resourceVersion := os.Getenv("CM_RESOURCE_VERSION")

	wi, err := wc.Watch(ctx, &corev1.ConfigMapList{}, &client.ListOptions{
		Raw: &metav1.ListOptions{
			ResourceVersion:     resourceVersion,
			AllowWatchBookmarks: true,
			SendInitialEvents:   ptr.To(true),
		},
	})

	if err != nil {
		l.Error("error watching", "error", err)
		os.Exit(1)
	}

	l.Info("ConfigMap watch successful", "resourceVersion", resourceVersion)

	for event := range wi.ResultChan() {
		pod := event.Object.(*corev1.ConfigMap)
		l.Info("ConfigMap event", "type", event.Type, "name", pod.Name, "namespace", pod.Namespace, "resourceVersion", pod.ResourceVersion)
	}
}
