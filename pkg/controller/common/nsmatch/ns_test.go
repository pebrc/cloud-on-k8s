// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package nsmatch

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const testOperatorNS = "elastic-system"

func mustSelector(t *testing.T, matchLabels map[string]string) labels.Selector {
	t.Helper()
	ls := metav1.LabelSelector{MatchLabels: matchLabels}
	sel, err := metav1.LabelSelectorAsSelector(&ls)
	if err != nil {
		t.Fatalf("LabelSelectorAsSelector error %s", err.Error())
	}
	return sel
}

func namespace(name string, lbls map[string]string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: lbls},
	}
}

// matcherWithNamespaces returns a matcher whose cache-backed reader contains the given namespaces.
func matcherWithNamespaces(sel labels.Selector, nss ...*corev1.Namespace) *NamespaceMatcher {
	m := NewNamespaceMatcher(sel, testOperatorNS)
	objs := make([]client.Object, len(nss))
	for i, ns := range nss {
		objs[i] = ns
	}
	m.SetCache(fake.NewClientBuilder().WithObjects(objs...).Build())
	return m
}

func TestSelectorEnabled(t *testing.T) {
	assert.False(t, (*NamespaceMatcher)(nil).SelectorEnabled(), "nil receiver is disabled")
	assert.False(t, NewNamespaceMatcher(nil, testOperatorNS).SelectorEnabled(), "nil selector is disabled")
	assert.True(t, NewNamespaceMatcher(mustSelector(t, map[string]string{"env": "prod"}), testOperatorNS).SelectorEnabled(), "non-nil selector is enabled")
}

func TestEvaluateLabels(t *testing.T) {
	sel := mustSelector(t, map[string]string{"env": "prod"})

	t.Run("selector disabled: always true", func(t *testing.T) {
		m := NewNamespaceMatcher(nil, testOperatorNS)
		assert.True(t, m.EvaluateLabels(namespace("any-ns", nil)))
	})

	t.Run("operator namespace bypasses evaluation", func(t *testing.T) {
		m := NewNamespaceMatcher(sel, testOperatorNS)
		assert.True(t, m.EvaluateLabels(namespace(testOperatorNS, nil)))
	})

	t.Run("matching labels", func(t *testing.T) {
		m := NewNamespaceMatcher(sel, testOperatorNS)
		assert.True(t, m.EvaluateLabels(namespace("prod-ns", map[string]string{"env": "prod"})))
	})

	t.Run("non-matching labels", func(t *testing.T) {
		m := NewNamespaceMatcher(sel, testOperatorNS)
		assert.False(t, m.EvaluateLabels(namespace("dev-ns", map[string]string{"env": "dev"})))
		assert.False(t, m.EvaluateLabels(namespace("unlabelled-ns", nil)))
	})
}

func TestMatches(t *testing.T) {
	sel := mustSelector(t, map[string]string{"env": "prod"})

	t.Run("selector disabled: always true, no cache needed", func(t *testing.T) {
		m := NewNamespaceMatcher(nil, testOperatorNS)
		assert.True(t, m.Matches(t.Context(), "any-ns"))
	})

	t.Run("empty namespace (cluster-scoped) always matches, no cache needed", func(t *testing.T) {
		m := NewNamespaceMatcher(sel, testOperatorNS)
		assert.True(t, m.Matches(t.Context(), ""))
	})

	t.Run("operator namespace always matches, no cache needed", func(t *testing.T) {
		m := NewNamespaceMatcher(sel, testOperatorNS)
		assert.True(t, m.Matches(t.Context(), testOperatorNS))
	})

	t.Run("matching labels in cache", func(t *testing.T) {
		m := matcherWithNamespaces(sel, namespace("prod-ns", map[string]string{"env": "prod"}))
		assert.True(t, m.Matches(t.Context(), "prod-ns"))
	})

	t.Run("non-matching labels in cache", func(t *testing.T) {
		m := matcherWithNamespaces(sel, namespace("dev-ns", map[string]string{"env": "dev"}))
		assert.False(t, m.Matches(t.Context(), "dev-ns"))
	})

	t.Run("namespace not in cache (deleted or unknown)", func(t *testing.T) {
		m := matcherWithNamespaces(sel)
		assert.False(t, m.Matches(t.Context(), "ghost-ns"))
	})

	t.Run("label change is observed immediately", func(t *testing.T) {
		cl := fake.NewClientBuilder().WithObjects(namespace("flip-ns", nil)).Build()
		m := NewNamespaceMatcher(sel, testOperatorNS)
		m.SetCache(cl)
		assert.False(t, m.Matches(t.Context(), "flip-ns"))

		var ns corev1.Namespace
		require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Name: "flip-ns"}, &ns))
		ns.Labels = map[string]string{"env": "prod"}
		require.NoError(t, cl.Update(t.Context(), &ns))
		assert.True(t, m.Matches(t.Context(), "flip-ns"))
	})
}

func TestMatchingNamespacesFromCache(t *testing.T) {
	sel := mustSelector(t, map[string]string{"env": "prod"})

	t.Run("returns matching namespaces plus the operator namespace", func(t *testing.T) {
		m := matcherWithNamespaces(sel,
			namespace("prod-1", map[string]string{"env": "prod"}),
			namespace("prod-2", map[string]string{"env": "prod"}),
			namespace("dev-ns", map[string]string{"env": "dev"}),
			namespace(testOperatorNS, nil),
		)
		names, err := m.MatchingNamespacesFromCache(t.Context())
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"prod-1", "prod-2", testOperatorNS}, names)
	})

	t.Run("selector disabled: all namespaces returned", func(t *testing.T) {
		m := matcherWithNamespaces(nil,
			namespace("ns-1", nil),
			namespace("ns-2", nil),
		)
		names, err := m.MatchingNamespacesFromCache(t.Context())
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"ns-1", "ns-2"}, names)
	})

	t.Run("list error is returned", func(t *testing.T) {
		m := NewNamespaceMatcher(sel, testOperatorNS)
		m.SetCache(fake.NewClientBuilder().WithInterceptorFuncs(interceptor.Funcs{
			List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
				return errors.New("cache unavailable")
			},
		}).Build())
		_, err := m.MatchingNamespacesFromCache(t.Context())
		require.Error(t, err)
	})
}
