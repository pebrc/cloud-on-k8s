// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package watches

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// WatchNamespaceFlips registers a Namespace watch that fires when a namespace's
// match state against the configured selector changes. On each such transition
// the watch lists all objects of the kind produced by newList in that namespace
// and enqueues a reconcile request for each. Create events for matching
// namespaces also fire: on startup the informer's initial sync delivers one per
// existing matching namespace, which replays every managed object through the
// controller — no separate seeding step is needed. No-ops when the matcher is
// disabled (legacy / static-namespace mode).
func WatchNamespaceFlips(
	c controller.Controller,
	ch cache.Cache,
	matcher NamespaceEvaluator,
	newList func() client.ObjectList,
) error {
	return WatchNamespaceFlipsMapped(c, ch, matcher,
		func(ctx context.Context, ns *corev1.Namespace) []reconcile.Request {
			list := newList()
			// Use the cache directly (not the FilterClient) so that resources in
			// namespaces being de-scoped are still visible here. The FilterClient
			// would silently drop them because the namespace no longer matches the
			// selector, causing us to miss the reconcile requests needed to clean up.
			if err := ch.List(ctx, list, client.InNamespace(ns.Name)); err != nil {
				return nil
			}
			items, err := apimeta.ExtractList(list)
			if err != nil {
				return nil
			}
			reqs := make([]reconcile.Request, 0, len(items))
			for _, item := range items {
				obj, ok := item.(client.Object)
				if !ok {
					continue
				}
				reqs = append(reqs, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: obj.GetNamespace(),
						Name:      obj.GetName(),
					},
				})
			}
			return reqs
		},
	)
}

// WatchNamespaceFlipsMapped registers a Namespace watch with a caller-provided
// mapper deciding which reconcile requests a namespace match-state change
// translates into. Use this instead of WatchNamespaceFlips when the objects to
// re-enqueue are not simply the ones living in the flipped namespace (e.g.
// associations referencing resources in that namespace).
//
// Match-state transitions are derived per event, with no stored state: update
// events carry both the old and the new object, so "started matching" and
// "stopped matching" are computed by evaluating the selector against both.
// Create events fire for matching namespaces (covering both newly created
// namespaces and the informer's initial sync); delete events are ignored —
// resources in a deleted namespace are being deleted with it.
//
// No-ops when the matcher is disabled (legacy / static-namespace mode).
func WatchNamespaceFlipsMapped(
	c controller.Controller,
	ch cache.Cache,
	matcher NamespaceEvaluator,
	mapFn func(context.Context, *corev1.Namespace) []reconcile.Request,
) error {
	if matcher == nil || !matcher.SelectorEnabled() {
		return nil
	}
	return c.Watch(source.Kind(ch, &corev1.Namespace{},
		handler.TypedEnqueueRequestsFromMapFunc(mapFn),
		predicate.TypedFuncs[*corev1.Namespace]{
			CreateFunc: func(e event.TypedCreateEvent[*corev1.Namespace]) bool {
				return matcher.EvaluateLabels(e.Object)
			},
			UpdateFunc: func(e event.TypedUpdateEvent[*corev1.Namespace]) bool {
				return matcher.EvaluateLabels(e.ObjectOld) != matcher.EvaluateLabels(e.ObjectNew)
			},
			DeleteFunc: func(event.TypedDeleteEvent[*corev1.Namespace]) bool {
				return false
			},
			GenericFunc: func(event.TypedGenericEvent[*corev1.Namespace]) bool {
				return false
			},
		},
	))
}

// NamespaceEvaluator is the subset of nsmatch.NamespaceMatcher needed to derive
// namespace match-state transitions from watch events.
type NamespaceEvaluator interface {
	SelectorEnabled() bool
	EvaluateLabels(*corev1.Namespace) bool
}
