// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package nsmatch

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NamespaceMatcher evaluates the configured label selector against Namespace
// objects read from the manager's cache. It holds no match state of its own:
// whether a namespace is managed is derived from cluster state (the namespace's
// current labels) at the moment of each call, so there is nothing to seed,
// broadcast, or keep in sync across replicas — every replica's cache answers
// for itself.
//
// When the selector is nil (disabled) the matcher is a no-op: Matches always
// returns true. This preserves legacy / static-resolution behaviour without
// any code changes in callers.
//
// Two namespaces bypass selector evaluation: the empty string (cluster-scoped
// objects) and the operator's own namespace — both always match so the
// operator can reconcile its own resources regardless of the configured
// selector.
type NamespaceMatcher struct {
	reader                  client.Reader
	selector                labels.Selector
	alwaysManagedNamespaces map[string]struct{} // namespaces excluded from label-selector evaluation.
}

// NewNamespaceMatcher returns a NamespaceMatcher. When sel is nil it acts as
// a no-op (Matches always returns true). Both the empty string (cluster-scoped
// resources) and operatorNS are pre-seeded in the short-circuit set so that
// cluster-scoped events and the operator's own namespace always match regardless
// of the configured selector.
func NewNamespaceMatcher(sel labels.Selector, operatorNS string) *NamespaceMatcher {
	return &NamespaceMatcher{
		selector: sel,
		alwaysManagedNamespaces: map[string]struct{}{
			"":         {},
			operatorNS: {},
		},
	}
}

// SetCache wires in the manager's cache-backed reader after manager creation
// (the cache is not available at construction time). Required before Matches
// and MatchingNamespacesFromCache can be used in dynamic mode.
func (m *NamespaceMatcher) SetCache(r client.Reader) {
	m.reader = r
}

// SelectorEnabled reports whether the matcher is actively filtering.
// Safe to call on a nil receiver; returns false in that case.
func (m *NamespaceMatcher) SelectorEnabled() bool {
	return m != nil && m.selector != nil
}

// EvaluateLabels evaluates the selector against the given Namespace object's
// labels, without any cache access. Intended for watch predicates that already
// hold the Namespace object (e.g. update events carrying both old and new
// objects, from which a match-state transition can be derived directly).
func (m *NamespaceMatcher) EvaluateLabels(ns *corev1.Namespace) bool {
	if !m.SelectorEnabled() {
		return true
	}
	if _, ok := m.alwaysManagedNamespaces[ns.Name]; ok {
		return true
	}
	return m.selector.Matches(labels.Set(ns.Labels))
}

// Matches fetches the Namespace named ns from the cache and evaluates the
// selector against its current labels. Returns false when the cache lookup
// fails (including a deleted namespace). When the selector is disabled,
// always returns true.
func (m *NamespaceMatcher) Matches(ctx context.Context, ns string) bool {
	if !m.SelectorEnabled() {
		return true
	}
	if _, ok := m.alwaysManagedNamespaces[ns]; ok {
		return true
	}
	var nsObj corev1.Namespace
	if err := m.reader.Get(ctx, client.ObjectKey{Name: ns}, &nsObj); err != nil {
		return false
	}
	return m.selector.Matches(labels.Set(nsObj.Labels))
}

// MatchingNamespacesFromCache lists all Namespace objects from the cache and
// returns the names of those whose labels satisfy the selector, plus the
// namespaces excluded from selector evaluation (the operator's own namespace),
// which are managed by definition. Returns an error if the cache list fails.
// When the selector is disabled, returns all namespace names.
func (m *NamespaceMatcher) MatchingNamespacesFromCache(ctx context.Context) ([]string, error) {
	var nsList corev1.NamespaceList
	if err := m.reader.List(ctx, &nsList); err != nil {
		return nil, err
	}

	names := make([]string, 0, len(nsList.Items))

	for i := range nsList.Items {
		if m.EvaluateLabels(&nsList.Items[i]) {
			names = append(names, nsList.Items[i].Name)
		}
	}

	return names, nil
}
