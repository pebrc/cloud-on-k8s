// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package runner

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"text/template"
	"time"
)

const (
	OcpDriverID                     = "ocp"
	OcpVaultPath                    = "secret/devops-ci/cloud-on-k8s/ci-ocp-k8s-operator"
	OcpServiceAccountVaultFieldName = "service-account"
	OcpPullSecretFieldName          = "pull-secret"
	OcpStateBucket                  = "eck-deployer-ocp-clusters-state"
	OcpConfigFileName               = "deployer-config-ocp.yml"
	DefaultOcpRunConfigTemplate     = `id: ocp-dev
overrides:
  clusterName: %s-dev-cluster
  ocp:
    gCloudProject: %s
    pullSecret: '%s'
`

	OcpInstallerConfigTemplate = `apiVersion: v1
baseDomain: {{.BaseDomain}}
compute:
- hyperthreading: Enabled
  name: worker
  platform:
    gcp:
      type: {{.MachineType}}
  replicas: {{.NodeCount}}
controlPlane:
  hyperthreading: Enabled
  name: master
  platform:
    gcp:
      type: {{.MachineType}}
  replicas: {{.NodeCount}}
metadata:
  creationTimestamp: null
  name: {{.ClusterName}}
networking:
  clusterNetwork:
  - cidr: 10.128.0.0/14
    hostPrefix: 23
  machineCIDR: 10.0.0.0/16
  networkType: OpenShiftSDN
  serviceNetwork:
  - 172.30.0.0/16
platform:
  gcp:
    projectID: {{.GCloudProject}}
    region: {{.Region}}
pullSecret: '{{.PullSecret}}'
sshKey: ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAACAQC5sHfihtwUUH+QGJqNebGWWHRlv2l1iDYD2CT6bsgCaU0en61V7z+mi4n85PiiH70CNjRUqXd53zEiXL5V8jFA3ERqnrTveh/wGztWefuD0nMNQMEUQdYMaxdW+tNbHtzVTNws9WeS06KwCE3TtKg5UK6MGH3WoI5hR3V2PDLhMXOvDxaUnGNc9B+0hAKnBXnBU9W4T90y/FgIe1t7FmQibzmqI3sl4TxMcEdc8ZQnQDQQDstRWssS5VvVI2i6y458TUBeahuU5cynAaBAqDM+a4FJcMmBAZUUvf6EmeK2siAQCIMflmj2ADNysaOwJfF3BnwPBMqLt+FF5pLuZxkyAmepvrDCTBujiaMtb8LukT38LyDXuyiPKv5RcrI6ygLy8mJujJYY89N8FmE7rXQ8zJVaGE9WObpQCJxAUM4jVkDHPkz7m/M3C2OPC9+g5cr0l49IYdRXvwPlNS+Rhm/zBl1LqKmotuuIF8tT80BETOAZn27GC8USZNieVTbGf4mRadkUJAnwpkqTBBaNXSXOwRJyLytImVhLac+XHFtiCvLcZa9vKFIf4Qbgg/6kKc7em9IzJJfwVvUI6HSth/bhi87pcdX3pyx8zpVmfCFYXtXy4lVH0DKXQo+DOX8KrCZT9zqgoKZo0P1cX4K2tnjX2lukX41KX9WQb9SkDsvA+Q== peter.brachwitz@elastic.co`
)

func init() {
	drivers[OcpDriverID] = &OcpDriverFactory{}
}

type OcpDriverFactory struct {
}

type runtimeState struct {
	// Authenticated tracks authentication against the GCloud API to avoid double authentication.
	Authenticated bool
	// SafeToDeleteWorkdir indicates that the installer state has been uploaded successfully to the storage bucket or is
	// otherwise not needed anymore.
	SafeToDeleteWorkdir bool
	// ClusterStateDir is the effective work dir containing the OCP installer state. Derived from plan.Ocp.Workdir.
	ClusterStateDir string
	// ClientImage is the name of the installer client image.
	ClientImage string
}

type OcpDriver struct {
	plan         Plan
	runtimeState runtimeState
}

func (*OcpDriverFactory) Create(plan Plan) (Driver, error) {
	return &OcpDriver{
		plan: plan,
	}, nil
}

func (d *OcpDriver) setup() []func() error {
	return []func() error{
		d.ensureWorkDir,
		d.authToGCP,
		d.ensureSSHDir,
		d.ensurePullSecret,
		d.downloadClusterState,
	}
}

func (d *OcpDriver) Execute() error {
	// client image requires a plan which we don't have in GetCredentials
	setup := append(d.setup(), d.ensureClientImage)

	if err := run(setup); err != nil {
		return err
	}

	defer func() {
		_ = d.removeWorkDir()
	}()

	clusterStatus := d.currentStatus()

	switch d.plan.Operation {
	case DeleteAction:
		if clusterStatus != NotFound {
			// always attempt a deletion
			return d.delete()
		}
		log.Printf("Not deleting as cluster doesn't exist")
	case CreateAction:
		if clusterStatus == Running {
			log.Printf("Not creating as cluster exists")
			// rsync sometimes get stuck this makes sure we retry upload on repeated create invocations
			if err := d.uploadClusterState(); err != nil {
				return err
			}
		} else if err := d.create(); err != nil {
			return err
		}

		return run([]func() error{
			d.copyKubeconfig,
			d.setupDisks,
			createStorageClass,
		})
	default:
		return fmt.Errorf("unknown operation %s", d.plan.Operation)
	}
	return nil
}

func (d *OcpDriver) create() error {
	log.Println("Creating cluster...")
	params := map[string]interface{}{
		"GCloudProject":     d.plan.Ocp.GCloudProject,
		"ClusterName":       d.plan.ClusterName,
		"Region":            d.plan.Ocp.Region,
		"AdminUsername":     d.plan.Ocp.AdminUsername,
		"KubernetesVersion": d.plan.KubernetesVersion,
		"MachineType":       d.plan.MachineType,
		"LocalSsdCount":     d.plan.Ocp.LocalSsdCount,
		"NodeCount":         d.plan.Ocp.NodeCount,
		"BaseDomain":        d.baseDomain(),
		"OcpStateBucket":    OcpStateBucket,
		"PullSecret":        d.plan.Ocp.PullSecret,
	}
	var tpl bytes.Buffer
	if err := template.Must(template.New("").Parse(OcpInstallerConfigTemplate)).Execute(&tpl, params); err != nil {
		return err
	}

	installConfig := filepath.Join(d.runtimeState.ClusterStateDir, "install-config.yaml")
	err := ioutil.WriteFile(installConfig, tpl.Bytes(), 0600)
	if err != nil {
		return err
	}

	err = d.runInstallerCommand("create")

	// We want to *always* upload the state of the cluster
	// this way we can run a delete operation even on failed
	// deployments to clean all the resources on GCP.
	_ = d.uploadClusterState()
	return err
}

func (d *OcpDriver) delete() error {
	log.Println("Deleting cluster...")

	err := d.runInstallerCommand("destroy")
	if err != nil {
		return err
	}

	// No need to check whether this `rm` command succeeds
	_ = NewCommand("gsutil rm -r gs://{{.OcpStateBucket}}/{{.ClusterName}}").AsTemplate(d.bucketParams()).WithoutStreaming().Run()
	d.runtimeState.SafeToDeleteWorkdir = true
	return nil
}

func (d *OcpDriver) GetCredentials() error {
	if err := run(d.setup()); err != nil {
		return err
	}

	defer func() {
		_ = d.removeWorkDir()
	}()

	return d.copyKubeconfig()
}

func run(steps []func() error) error {
	for _, fn := range steps {
		if err := fn(); err != nil {
			return err
		}
	}
	return nil
}

func (d *OcpDriver) setupDisks() error {
	return setupDisks(d.plan)
}

func (d *OcpDriver) ensureClientImage() error {
	image, err := ensureClientImage(OcpDriverID, d.plan.ClientVersion, d.plan.ClientBuildDefDir)
	if err != nil {
		return err
	}
	d.runtimeState.ClientImage = image
	return nil
}

func (d *OcpDriver) ensurePullSecret() error {
	if d.plan.Ocp.PullSecret == "" {
		client, err := NewClient(*d.plan.VaultInfo)
		if err != nil {
			return err
		}
		s, err := client.Get(OcpVaultPath, OcpPullSecretFieldName)
		if err != nil {
			return err
		}
		d.plan.Ocp.PullSecret = s
	}
	return nil
}

func (d *OcpDriver) ensureWorkDir() error {
	if d.runtimeState.ClusterStateDir != "" {
		// already initialised
		return nil
	}
	workDir := d.plan.Ocp.WorkDir
	if workDir == "" {
		// base work dir in HOME dir otherwise mounting to container won't work without further settings adjustment
		// in macOS in local mode. In CI mode we need the workdir to be in the volume shared between containers.
		// having the work dir in HOME also underlines the importance of the work dir contents. The work dir is the only
		// source to cleanly uninstall the cluster should the rsync fail.
		var err error
		workDir, err = ioutil.TempDir(os.Getenv("HOME"), d.plan.ClusterName)
		if err != nil {
			return err
		}
		log.Printf("Defaulting WorkDir: %s", workDir)
	}

	if err := os.MkdirAll(workDir, os.ModePerm); err != nil {
		return err
	}
	d.runtimeState.ClusterStateDir = workDir
	log.Printf("Using ClusterStateDir: %s", workDir)
	return nil
}

func (d *OcpDriver) removeWorkDir() error {
	if !d.runtimeState.SafeToDeleteWorkdir {
		log.Printf("Not deleting work dir as rsync backup of installer state not successful")
		return nil
	}
	// keep workdir around useful for debugging or when running in non-CI mode
	if d.plan.Ocp.StickyWorkDir {
		log.Printf("Not deleting work dir as requested via StickyWorkDir option")
		return nil
	}
	return os.RemoveAll(d.plan.Ocp.WorkDir)
}

func (d *OcpDriver) authToGCP() error {
	// avoid double authentication
	if d.runtimeState.Authenticated {
		return nil
	}

	if err := authToGCP(
		d.plan.VaultInfo, OcpVaultPath, OcpServiceAccountVaultFieldName,
		d.plan.ServiceAccount, false, d.plan.Ocp.GCloudProject,
	); err != nil {
		return err
	}
	d.runtimeState.Authenticated = true
	return nil
}

func (d *OcpDriver) ensureSSHDir() error {
	sshDir := filepath.Join(os.Getenv("HOME"), ".ssh")
	if _, err := os.Stat(sshDir); os.IsNotExist(err) {
		log.Printf("Creating .ssh directory")
		return os.Mkdir(sshDir, 0700)
	}
	return nil
}

type ClusterStatus string

var (
	PartiallyDeployed ClusterStatus = "PartiallyDeployed"
	NotFound          ClusterStatus = "NotFound"
	NotResponding     ClusterStatus = "NotResponding"
	Running           ClusterStatus = "Running"
)

func (d *OcpDriver) currentStatus() ClusterStatus {
	log.Println("Checking if cluster exists...")

	kubeConfig := filepath.Join(d.runtimeState.ClusterStateDir, "auth", "kubeconfig")
	if _, err := os.Stat(kubeConfig); os.IsNotExist(err) {
		if empty, err := isEmpty(d.runtimeState.ClusterStateDir); empty && err == nil {
			return NotFound
		}
		return PartiallyDeployed
	}

	log.Println("Cluster state synced: Testing that the OpenShift cluster is alive... ")
	cmd := "kubectl version"
	alive, err := NewCommand(cmd).WithoutStreaming().WithVariable("KUBECONFIG", kubeConfig).OutputContainsAny("Server Version")

	if !alive || err != nil { // error will be typically not nil when alive is false but let's be explicit here to avoid returning Running on a non-nil error
		log.Printf("a cluster state dir was found in %s but the cluster is not responding to `kubectl version`: %s", d.runtimeState.ClusterStateDir, err.Error())
		return NotResponding
	}

	return Running
}

func isEmpty(dir string) (bool, error) {
	// https://stackoverflow.com/questions/30697324/how-to-check-if-directory-on-path-is-empty
	f, err := os.Open(dir)
	if err != nil {
		return false, err
	}
	defer f.Close()

	_, err = f.Readdirnames(1)
	if errors.Is(err, io.EOF) {
		return true, nil
	}
	return false, err
}

func (d *OcpDriver) uploadClusterState() error {
	// Let's check that the cluster dir exists
	// before we attempt an upload.
	if _, err := os.Stat(d.runtimeState.ClusterStateDir); os.IsNotExist(err) {
		log.Printf("clusterStateDir %s not present", d.runtimeState.ClusterStateDir)
		return nil
	}

	bucketNotFound, err := NewCommand("gsutil ls gs://{{.OcpStateBucket}}").
		AsTemplate(d.bucketParams()).
		WithoutStreaming().
		OutputContainsAny("BucketNotFoundException")
	if err != nil {
		return fmt.Errorf("while checking state bucket existence %w", err)
	}
	if bucketNotFound {
		if err := NewCommand("gsutil mb gs://{{.OcpStateBucket}}").AsTemplate(d.bucketParams()).Run(); err != nil {
			return fmt.Errorf("while creating storage bucket: %w", err)
		}
	}

	// rsync seems to get stuck at least in local mode every now and then let's retry a few times
	err = NewCommand("gsutil rsync -r -d {{.ClusterStateDir}} gs://{{.OcpStateBucket}}/{{.ClusterName}}").
		WithLog("Uploading cluster state").
		AsTemplate(d.bucketParams()).
		WithoutStreaming().
		RunWithRetries(3, 15*time.Minute)
	if err == nil {
		d.runtimeState.SafeToDeleteWorkdir = true
	}
	return err
}

func (d *OcpDriver) downloadClusterState() error {
	cmd := "gsutil rsync -r -d gs://{{.OcpStateBucket}}/{{.ClusterName}} {{.ClusterStateDir}}"
	doesNotExist, err := NewCommand(cmd).
		AsTemplate(d.bucketParams()).
		WithLog("Synching cluster state").
		WithoutStreaming().
		OutputContainsAny("BucketNotFoundException", "does not name a directory, bucket, or bucket subdir")
	if doesNotExist {
		log.Printf("No remote cluster state found")
		return nil // swallow this error as it is expected if no cluster has been created yet
	}
	return err
}

func (d *OcpDriver) copyKubeconfig() error {
	log.Printf("Copying  credentials")
	kubeConfig := filepath.Join(d.runtimeState.ClusterStateDir, "auth", "kubeconfig")

	// 1. do we have something to copy?
	if _, err := os.Stat(kubeConfig); os.IsNotExist(err) {
		return errors.New("OpenShift's kubeconfig file does not exist")
	}

	// 2. is there any existing kubeconfig?
	hostKubeconfig := filepath.Join(os.Getenv("HOME"), ".kube", "config")
	if _, err := os.Stat(hostKubeconfig); os.IsNotExist(err) {
		// if no just copy it over
		return copyFile(kubeConfig, hostKubeconfig)
	}
	// 3. if there is existing configuration  attempt to merge both
	merged, err := NewCommand("kubectl config view --flatten").
		WithLog("Merging kubeconfig with").
		WithoutStreaming().
		WithVariable("KUBECONFIG", fmt.Sprintf("%s:%s", hostKubeconfig, kubeConfig)).
		Output()
	if err != nil {
		return err
	}

	if err := ioutil.WriteFile(hostKubeconfig, []byte(merged), 0600); err != nil {
		return err
	}
	// 4. after merging make sure that the ocp context, which is always called `admin`
	return NewCommand("kubectl config use-context admin").Run()
}

func copyFile(src, tgt string) error {
	if err := os.MkdirAll(filepath.Dir(tgt), os.ModePerm); err != nil {
		return err
	}
	cmd := fmt.Sprintf("cp %s %s", src, tgt)
	return NewCommand(cmd).WithoutStreaming().WithLog("Copying kubeconfig").Run()
}

func (d *OcpDriver) bucketParams() map[string]interface{} {
	return map[string]interface{}{
		"OcpStateBucket":  OcpStateBucket,
		"ClusterName":     d.plan.ClusterName,
		"ClusterStateDir": d.runtimeState.ClusterStateDir,
	}
}

func (d *OcpDriver) runInstallerCommand(action string) error {
	params := map[string]interface{}{
		"ClusterStateDirBase": filepath.Base(d.runtimeState.ClusterStateDir),
		"SharedVolume":        SharedVolumeName(),
		"GCloudCredsPath":     filepath.Join("/home", GCPDir, ServiceAccountFilename),
		"OCPToolsDockerImage": d.runtimeState.ClientImage,
		"Action":              action,
	}
	// We are mounting the shared volume into the installer container and configure it to be the HOME directory
	// this is mainly so that the GCloud tooling picks up the authentication information correctly as the base image is
	// scratch+curl and thus an empty
	// We are mounting tmp as the installer needs a scratch space and writing into the container won't work
	cmd := NewCommand(`docker run --rm \
		-v {{.SharedVolume}}:/home \
		-v /tmp:/tmp \
		-e GOOGLE_APPLICATION_CREDENTIALS={{.GCloudCredsPath}} \
		-e HOME=/home \
		{{.OCPToolsDockerImage}} \
		/openshift-install {{.Action}} cluster --dir /home/{{.ClusterStateDirBase}} --log-level debug`)
	return cmd.AsTemplate(params).Run()
}

func (d *OcpDriver) baseDomain() string {
	baseDomain := d.plan.Ocp.BaseDomain
	// Domains used for the OCP deployment must be
	// pre-configured on the destination cloud. A zone
	// for these domains must exist and it has to be
	// reachable from the internet as `openshift-installer`
	// interacts with the deployed OCP cluster to monitor
	// and complete the deployment.
	//
	// The default `eck-ocp.elastic.dev` subdomain is configured
	// on AWS as an NS record and points to a zone configured in
	// the `elastic-cloud-dev` project on GCP.
	if baseDomain == "" {
		baseDomain = "eck-ocp.elastic.dev"
	}
	return baseDomain
}
