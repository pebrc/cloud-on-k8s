package internal

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp" // auth on gke
)

type DumpParams struct {
	Kubeconfig          string
	OperatorNamespaces  []string
	ResourcesNamespaces []string
	OutputDir           string
	CreateZip           bool
	Verbose             bool
}

func (dp DumpParams) AllNamespaces() []string {
	nss := make([]string, 0, len(dp.ResourcesNamespaces)+len(dp.OperatorNamespaces))
	nss = append(nss, dp.ResourcesNamespaces...)
	nss = append(nss, dp.OperatorNamespaces...)
	return nss
}

func RunDump(params DumpParams) error {
	kubectl, err := NewKubectl(genericclioptions.NewConfigFlags(true))
	if err != nil {
		return err
	}
	if err := kubectl.CheckNamespaces(context.Background(), params.AllNamespaces()); err != nil {
		return err
	}

	zipFile, err := NewZipFile(params.OutputDir)
	if err != nil {
		return err
	}
	zipFile.add(map[string]func(io.Writer) error{
		"nodes.json": func(writer io.Writer) error {
			return kubectl.Get("nodes", "", writer)
		},
		"podsecuritypolicies.json": func(writer io.Writer) error {
			return kubectl.Get("podsecuritypolicies", "", writer)
		},
		"clusterroles.txt": func(writer io.Writer) error {
			return kubectl.Describe("clusterroles", "elastic", "", writer)
		},
	})

	for _, ns := range params.OperatorNamespaces {
		zipFile.add(getResources(kubectl, ns, []string{
			"statefulsets",
			"pods",
			"services",
			"configmaps",
			"events",
			"networkpolicies",
			"controllerrevisions",
		}))
		zipFile.add(map[string]func(io.Writer) error{
			filepath.Join(ns, "secrets.json"): func(writer io.Writer) error {
				return kubectl.GetMeta("secrets", ns, writer)
			},
		})
	}

	for _, ns := range params.ResourcesNamespaces {
		zipFile.add(getResources(kubectl, ns, []string{
			"statefulsets",
			"replicasets",
			"deployments",
			"daemonsets",
			"pods",
			"persistentvolumes",
			"persistentvolumeclaims",
			"services",
			"endpoints",
			"configmaps",
			"events",
			"networkpolicies",
			"controllerrevisions",
		}))
	}

	return zipFile.Close()
}

func getResources(k *Kubectl, ns string, rs []string) map[string]func(io.Writer) error {
	m := map[string]func(io.Writer) error{}
	for _, r := range rs {
		resource := r
		m[filepath.Join(ns, resource+".json")] = func(w io.Writer) error {
			return k.Get(resource, ns, w)
		}
	}
	return m
}

type ZipFile struct {
	zip        *zip.Writer
	underlying io.Closer
}

func NewZipFile(dir string) (*ZipFile, error) {
	f, err := os.Create(diagnosticFilename(dir))
	if err != nil {
		return nil, err
	}
	w := zip.NewWriter(f)
	return &ZipFile{
		zip:        w,
		underlying: f,
	}, nil

}

func (z ZipFile) Close() error {
	// TODO aggregate error?
	defer z.underlying.Close()
	return z.zip.Close()
}

func (z ZipFile) add(fns map[string]func(io.Writer) error) error {
	for k, f := range fns {
		fw, err := z.zip.Create(k)
		if err != nil {
			return err
		}
		if err := f(fw); err != nil {
			return err
		}
	}
	return nil
}

func diagnosticFilename(dir string) string {
	file := fmt.Sprintf("eck-diagnostic-%s.zip", time.Now().Format("2006-01-02T15-04-05"))
	if dir != "" {
		file = filepath.Join(dir, file)
	}
	return file
}
