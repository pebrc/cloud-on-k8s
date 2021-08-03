// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package internal

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/ghodss/yaml"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/kubectl/pkg/cmd/exec"
	"k8s.io/utils/pointer"
)

var (
	//go:embed job.tpl.yml
	jobTemplate        string
	jobTimeout         = 10 * time.Minute
	jobPollingInterval = 10 * time.Second
)

type diagJob struct {
	podName string
	esName  string
	done    bool
}

type diagJobState struct {
	ns         string
	clientSet  *kubernetes.Clientset
	config     *rest.Config
	informer   cache.SharedInformer
	jobs       map[string]*diagJob
	context    context.Context
	cancelFunc context.CancelFunc
	verbose    bool
}

func newDiagJobState(clientSet *kubernetes.Clientset, config *rest.Config, ns string, verbose bool) *diagJobState {
	ctx, cancelFunc := context.WithTimeout(context.Background(), jobTimeout)
	factory := informers.NewSharedInformerFactoryWithOptions(
		clientSet,
		jobPollingInterval,
		informers.WithNamespace(ns),
		informers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.LabelSelector = "app.kubernetes.io/name=eck-diagnostics"
		}))
	return &diagJobState{
		jobs:       map[string]*diagJob{},
		ns:         ns,
		clientSet:  clientSet,
		config:     config,
		informer:   factory.Core().V1().Pods().Informer(),
		cancelFunc: cancelFunc,
		context:    ctx,
		verbose:    verbose,
	}
}

func (ds *diagJobState) scheduleJob(esName string, tls bool) error {
	podName := fmt.Sprintf("%s-diag", esName)
	tpl, err := template.New("job").Parse(jobTemplate)
	if err != nil {
		return err
	}

	buffer := new(bytes.Buffer)
	err = tpl.Execute(buffer, map[string]interface{}{
		"PodName":     podName,
		"ESNamespace": ds.ns,
		"ESName":      esName,
		"TLS":         tls,
	})
	if err != nil {
		return err
	}

	var pod corev1.Pod
	err = yaml.Unmarshal(buffer.Bytes(), &pod)
	if err != nil {
		return err
	}

	// TODO deal with cache delay
	err = ds.clientSet.CoreV1().Pods(ds.ns).Delete(context.Background(), podName, metav1.DeleteOptions{GracePeriodSeconds: pointer.Int64Ptr(0)})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	_, err = ds.clientSet.CoreV1().Pods(ds.ns).Create(context.Background(), &pod, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	ds.jobs[podName] = &diagJob{
		podName: podName,
		esName:  esName,
	}
	return nil
}

func (ds *diagJobState) extractJobResults(file *ZipFile) error {
	ds.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			if pod, ok := obj.(*corev1.Pod); ok && ds.verbose {
				fmt.Printf("%s/%s added\n", pod.Namespace, pod.Name)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			pod, ok := newObj.(*corev1.Pod)
			if !ok {
				fmt.Printf("unexpected %v, expected type Pod\n", newObj)
				return
			}
			job, found := ds.jobs[pod.Name]
			if !found {
				fmt.Printf("unexpected no record for Pod %s/%s\n", pod.Namespace, pod.Name)
				return
			}

			if job.done {
				fmt.Printf("job already done for Pod %s/%s\n", pod.Namespace, pod.Name)
				return
			}

			switch pod.Status.Phase {
			case corev1.PodRunning:
				// extract logs
				reader, outStream := io.Pipe()
				options := &exec.ExecOptions{
					StreamOptions: exec.StreamOptions{
						IOStreams: genericclioptions.IOStreams{
							In:     nil,
							Out:    outStream,
							ErrOut: os.Stderr,
						},

						Namespace: pod.Namespace,
						PodName:   pod.Name,
					},
					Config:    ds.config,
					PodClient: ds.clientSet.CoreV1(),
					Command:   []string{"tar", "cf", "-", "/diagnostic-output"},
					Executor:  &exec.DefaultRemoteExecutor{},
				}
				go func() {
					defer outStream.Close()
					err := options.Run()
					if err != nil {
						println(err.Error())
						return
					}
				}()
				err := ds.untarIntoZip(reader, job.esName, file)
				if err != nil {
					// TODO error handling
					println(err.Error())
					return
				}
				err = ds.completeJob(job)
				if err != nil {
					println(err.Error())
					return
				}
			case corev1.PodSucceeded:
				fmt.Printf("unexpected: Pod %s/%s succeeded\n", pod.Namespace, pod.Name)
				job.done = true
			case corev1.PodFailed:
				fmt.Printf("unexpected: Pod %s/%s failed\n", pod.Namespace, pod.Name)
				job.done = true
			}
		},
		DeleteFunc: func(obj interface{}) {
			pod, ok := obj.(*corev1.Pod)
			if !ok {
				fmt.Printf("unexpected %v, expected type Pod\n", obj)
				return
			}

			if ds.verbose {
				fmt.Printf("%s/%s deleted\n", pod.Namespace, pod.Name)
			}

			done := true
			for _, j := range ds.jobs {
				if !j.done {
					done = false
				}
			}
			if done {
				ds.cancelFunc()
			}

		},
	})
	ds.informer.Run(ds.context.Done())
	return nil
}

func (ds *diagJobState) untarIntoZip(reader *io.PipeReader, esName string, file *ZipFile) error {
	tarReader := tar.NewReader(reader)
	for {
		header, err := tarReader.Next()
		if err != nil {
			if err != io.EOF {
				return err
			}
			break
		}
		remoteFilename := header.Name
		// remove the path prefix on the Pod
		remoteFilename = strings.TrimPrefix(remoteFilename, "diagnostic-output/")
		if !strings.HasPrefix(remoteFilename, "api-diagnostics") {
			if ds.verbose {
				fmt.Printf("ignoring file in tar from Pod %s\n", header.Name)
			}
			continue
		}
		if strings.HasSuffix(remoteFilename, "tar.gz") {
			err := ds.repackageTarGzip(tarReader, esName, file)
			if err != nil {
				return err
			}
		} else {
			out, err := file.Create(filepath.Join(ds.ns, "elasticsearch", esName, remoteFilename))
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tarReader); err != nil {
				return err
			}
		}

	}
	return nil

}

func (ds *diagJobState) completeJob(job *diagJob) error {
	if ds.verbose {
		fmt.Printf("Job %s complete\n", job.podName)
	}

	job.done = true
	return ds.clientSet.CoreV1().Pods(ds.ns).Delete(ds.context, job.podName, metav1.DeleteOptions{GracePeriodSeconds: pointer.Int64Ptr(0)})
}

func (ds *diagJobState) repackageTarGzip(in io.Reader, esName string, zipFile *ZipFile) error {
	gzReader, err := gzip.NewReader(in)
	if err != nil {
		return err
	}
	topLevelDir := ""
	tarReader := tar.NewReader(gzReader)
	for {
		header, err := tarReader.Next()
		if err != nil {
			if err != io.EOF {
				return err
			}
			break
		}
		println(header.Name)
		switch header.Typeflag {
		case tar.TypeDir:
			if topLevelDir == "" {
				println("new toplevel ^^")
				topLevelDir = header.Name
			}
			continue
		case tar.TypeReg:
			rel, err := filepath.Rel(topLevelDir, header.Name)
			if err != nil {
				return err
			}
			out, err := zipFile.Create(filepath.Join(ds.ns, "elasticsearch", esName, rel))
			if err != nil {
				return err
			}
			_, err = io.Copy(out, tarReader)
			if err != nil {
				return err
			}

		}
	}
	return nil
}

func runElasticsearchDiagnostics(k *Kubectl, ns string, zipFile *ZipFile, verbose bool) error {
	config, err := k.factory.ToRESTConfig()
	if err != nil {
		return err
	}
	clientSet, err := k.factory.KubernetesClientSet()
	if err != nil {
		return err
	}
	state := newDiagJobState(clientSet, config, ns, verbose)

	resources, err := k.getResources("elasticsearch", ns)
	if err != nil {
		return err
	}
	// var wg sync.WaitGroup
	if err := resources.Visit(func(info *resource.Info, err error) error {
		if err != nil {
			return err
		}

		esName := info.Name
		es, err := runtime.DefaultUnstructuredConverter.ToUnstructured(info.Object)
		if err != nil {
			return err
		}
		disabled, found, err := unstructured.NestedBool(es, "spec", "http", "tls", "selfSignedCertificate", "disabled")
		if err != nil {
			return err
		}
		tls := !(found && disabled)

		if err := state.scheduleJob(esName, tls); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	return state.extractJobResults(zipFile)
}
