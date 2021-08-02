// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License
// 2.0; you may not use this file except in compliance with the Elastic License
// 2.0.

package internal

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/describe"
	"k8s.io/kubectl/pkg/polymorphichelpers"
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

func (c Kubectl) GetMeta(resource, namespace string, w io.Writer) error {
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

	type MetaList struct {
		Items []interface{}
	}

	var metas MetaList
	metaAccess := meta.NewAccessor()
	infos, err := r.Infos()
	if err != nil {
		return err
	}
	for i := range infos {
		obj := infos[i].Object
		annotations, err := metaAccess.Annotations(obj)
		if err != nil {
			return err
		}
		// last-applied-configuration can contain sensitive data let's remove it
		delete(annotations, corev1.LastAppliedConfigAnnotation)
		metaAccess.SetAnnotations(obj, annotations)
		unstructured, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
		if err != nil {
			return err
		}
		// remove the actual secret data
		delete(unstructured, "data")
		// or spec for other objects
		delete(unstructured, "spec")
		metas.Items = append(metas.Items, unstructured)
	}
	bytes, err := json.MarshalIndent(metas, "", "    ")
	if err != nil {
		return err
	}
	_, err = w.Write(bytes)
	return err

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

func (c Kubectl) Logs(namespace string, selector string, out func(string) (io.Writer, error)) error {
	builder := c.factory.NewBuilder().
		WithScheme(scheme.Scheme, scheme.Scheme.PrioritizedVersionsAllGroups()...).
		NamespaceParam(namespace).
		SingleResourceType().ResourceTypes("pods")

	if selector != "" {
		builder.LabelSelector(selector)
	} else {
		builder.SelectAllParam(true)
	}
	infos, err := builder.Do().Infos()
	if err != nil {
		return err
	}

	for i := range infos {
		obj := infos[i].Object
		switch t := obj.(type) {
		case *corev1.PodList:
			for _, p := range t.Items {
				if err := c.requestLogs(p, out); err != nil {
					return err
				}
			}
		case *corev1.Pod:
			if err := c.requestLogs(*t, out); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c Kubectl) requestLogs(pod corev1.Pod, out func(string) (io.Writer, error)) error {
	logFn := polymorphichelpers.LogsForObjectFn
	reqs, err := logFn(c.factory, &pod, &corev1.PodLogOptions{}, 20*time.Second, true)
	if err != nil {
		return err
	}
	writer, err := out(filepath.Join(pod.Namespace, "pod", pod.Name, "logs.txt"))
	if err != nil {
		return err
	}
	for _, r := range reqs {
		if err := streamLogs(types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, r, writer); err != nil {
			return err
		}

	}
	return nil
}

func streamLogs(nsn types.NamespacedName, request rest.ResponseWrapper, out io.Writer) error {
	stream, err := request.Stream(context.Background())
	if err != nil {
		return err
	}
	defer stream.Close()
	out.Write([]byte(fmt.Sprintf("==== START logs for %s ====\n", nsn.String())))
	defer func() {
		out.Write([]byte(fmt.Sprintf("==== END logs for %s ====\n", nsn.String())))
	}()
	r := bufio.NewReader(stream)
	for {
		bytes, err := r.ReadBytes('\n')
		if _, err := out.Write(bytes); err != nil {
			return err
		}

		if err != nil {
			if err != io.EOF {
				return err
			}
			return nil
		}
	}
}
