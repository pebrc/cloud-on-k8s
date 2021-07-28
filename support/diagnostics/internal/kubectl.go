// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License
// 2.0; you may not use this file except in compliance with the Elastic License
// 2.0.

package internal

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"k8s.io/kubectl/pkg/describe"

	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/client-go/kubernetes/scheme"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/kubectl/pkg/util/openapi"
)

// Kubectl provides utilities based on the kubectl API.
type Kubectl struct {
	defaultNamespace string
	factory          cmdutil.Factory
	openAPISchema    openapi.Resources
	out              io.Writer
	errOut           io.Writer
}

// NewKubectl creates a new instance of Kubectl.
func NewKubectl(confFlags *genericclioptions.ConfigFlags) (*Kubectl, error) {
	matchVersionFlags := cmdutil.NewMatchVersionFlags(confFlags)
	factory := cmdutil.NewFactory(matchVersionFlags)

	openAPISchema, err := factory.OpenAPISchema()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve OpenAPI schema: %w", err)
	}

	return &Kubectl{
		defaultNamespace: "default",
		factory:          factory,
		openAPISchema:    openAPISchema,
		out:              os.Stdout,
		errOut:           os.Stderr,
	}, nil
}

func (c Kubectl) CheckNamespaces(ctx context.Context, nss []string) error {
	clientSet, err := c.factory.KubernetesClientSet()
	if err != nil {
		return err
	}
	for _, ns := range nss {
		if _, err := clientSet.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{}); err != nil {
			return err
		}
	}
	return nil
}

func (c Kubectl) Get(resource, namespace string, w io.Writer) error {
	r := c.factory.NewBuilder().
		Unstructured().
		NamespaceParam(namespace).DefaultNamespace().AllNamespaces(false).
		ResourceTypeOrNameArgs(true, resource).
		ContinueOnError().
		Latest().
		Flatten().
		Do()

	r.IgnoreErrors(apierrors.IsNotFound)
	if err := r.Err(); err != nil {
		return err
	}
	printer, err := printers.NewTypeSetter(scheme.Scheme).WrapToPrinter(&printers.JSONPrinter{}, nil)
	if err != nil {
		return err
	}

	obj, err := r.Object()
	if err != nil {
		return err
	}

	return printer.PrintObj(obj, w)
}

func (c Kubectl) Describe(resource, prefix, namespace string, w io.Writer) error {
	r := c.factory.NewBuilder().
		Unstructured().
		NamespaceParam(namespace).DefaultNamespace().AllNamespaces(false).
		ResourceTypeOrNameArgs(true, resource).
		ContinueOnError().
		Latest().
		Flatten().
		Do()
	if err := r.Err(); err != nil {
		return err
	}

	infos, err := r.Infos()
	if err != nil {
		return err
	}
	for _, info := range infos {
		if !strings.HasPrefix(info.Name, prefix) {
			continue
		}
		mapping := info.ResourceMapping()
		desc, err := describe.Describer(c.factory, mapping)
		if err != nil {
			return err
		}
		s, err := desc.Describe(info.Namespace, info.Name, describe.DescriberSettings{ShowEvents: true})
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "%s\n", s)
	}
	return nil
}
