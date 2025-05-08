package controllers

import (
	"context"
	krtlite "github.com/kalexmills/krt-lite"
	"k8s.io/apimachinery/pkg/api/errors"
	"log/slog"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// simpleReconciler is an event handler which performs simple CRUD operations for each event using the provided client.
// Any errors which occur are logged via slog.
func simpleReconciler[T client.Object](ctx context.Context, cli client.Client) func(ev krtlite.Event[T]) {
	slogArgs := func(err error, obj client.Object) []any {
		return []any{
			slog.String("err", err.Error()),
			slog.String("kind", obj.GetObjectKind().GroupVersionKind().String()),
			slog.String("name", obj.GetNamespace()),
			slog.String("namespace", obj.GetNamespace()),
		}
	}

	return func(ev krtlite.Event[T]) {
		obj := ev.Latest()

		switch ev.Type {
		case krtlite.EventAdd:
			err := cli.Create(ctx, obj)
			if err != nil {
				if !errors.IsAlreadyExists(err) {
					slog.ErrorContext(ctx, "error creating object", slogArgs(err, obj)...)
				}
			}
		case krtlite.EventUpdate:
			err := cli.Update(ctx, obj)
			if err != nil {
				slog.ErrorContext(ctx, "error updating object", slogArgs(err, obj)...)
			}
		case krtlite.EventDelete:
			err := cli.Delete(ctx, obj)
			if err != nil {
				if !errors.IsNotFound(err) {
					slog.ErrorContext(ctx, "error deleting object", slogArgs(err, obj)...)
				}
			}
		}
	}
}
