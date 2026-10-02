// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package kibana

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	commonv1 "github.com/elastic/cloud-on-k8s/v3/pkg/apis/common/v1"
	kbv1 "github.com/elastic/cloud-on-k8s/v3/pkg/apis/kibana/v1"
	"github.com/elastic/cloud-on-k8s/v3/pkg/controller/common"
	"github.com/elastic/cloud-on-k8s/v3/pkg/controller/common/deployment"
	"github.com/elastic/cloud-on-k8s/v3/pkg/controller/common/metadata"
	"github.com/elastic/cloud-on-k8s/v3/pkg/controller/common/settings"
	kblabel "github.com/elastic/cloud-on-k8s/v3/pkg/controller/kibana/label"
	ulog "github.com/elastic/cloud-on-k8s/v3/pkg/utils/log"
)

// poolPlan describes the desired state of one Kibana pool. It is deliberately
// controller-local: the API describes the requested split, while the controller
// decides which Deployments and supporting resources are required to implement it.
type poolPlan struct {
	role             kblabel.Role
	config           CanonicalConfig
	configSecretName string
	deploymentName   string
	identityLabels   map[string]string
	resources        commonv1.Resources
	podTemplate      corev1.PodTemplateSpec
	desiredReplicas  *int32
}

func buildPoolPlans(kb *kbv1.Kibana) ([]poolPlan, error) {
	roles := []kblabel.Role{kblabel.SinglePoolRole}
	if kb.BackgroundTasksEnabled() {
		roles = []kblabel.Role{kblabel.UIRole, kblabel.BackgroundTasksRole}
	}

	plans := make([]poolPlan, 0, len(roles))
	for _, role := range roles {
		plan, err := buildPoolPlan(kb, role)
		if err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

func buildPoolPlan(kb *kbv1.Kibana, role kblabel.Role) (poolPlan, error) {
	var specConfigData map[string]any
	if kb.Spec.Config != nil {
		specConfigData = kb.Spec.Config.Data
	}
	pcg, err := settings.NewCanonicalConfigFrom(specConfigData)
	if err != nil {
		return poolPlan{}, err
	}

	podTemplate, err := getPodTemplateSpecForRole(*kb, role)
	if err != nil {
		return poolPlan{}, err
	}

	plan := poolPlan{
		role:             role,
		config:           CanonicalConfig{pcg},
		configSecretName: kbv1.ConfigSecret(kb.Name),
		deploymentName:   kbv1.KBNamer.Suffix(kb.Name),
		identityLabels:   poolIdentityLabels(kb, role),
		resources:        kb.Spec.Resources,
		podTemplate:      podTemplate,
		desiredReplicas:  new(kb.Spec.Count),
	}

	switch {
	case role.IsSinglePool(), role.IsUI():
		return plan, nil
	case role.IsBackgroundTasks():
		if !kb.BackgroundTasksEnabled() {
			return poolPlan{}, fmt.Errorf("background tasks pool requested without spec.backgroundTasks")
		}
		plan.deploymentName = kbv1.BackgroundTasksDeployment(kb.Name)
		plan.desiredReplicas = kb.Spec.BackgroundTasks.Count
		if !kb.Spec.BackgroundTasks.Resources.IsEmpty() {
			plan.resources = kb.Spec.BackgroundTasks.Resources
		}
		if kb.Spec.BackgroundTasks.Config == nil {
			return plan, nil
		}
		plan.config, err = plan.config.WithOverlay(kb.Spec.BackgroundTasks.Config)
		if err != nil {
			return poolPlan{}, err
		}
		plan.configSecretName = kbv1.BackgroundTasksConfigSecret(kb.Name)
		return plan, nil
	default:
		return poolPlan{}, fmt.Errorf("unknown Kibana pool role: %+v", role)
	}
}

func poolIdentityLabels(kb *kbv1.Kibana, role kblabel.Role) map[string]string {
	labels := kb.GetIdentityLabels()
	labels[kblabel.RoleLabelName] = kblabel.RolePrimaryValue
	if kb.BackgroundTasksEnabled() && role.LabelValue != "" {
		labels[kblabel.RoleLabelName] = role.LabelValue
	}
	return labels
}

type poolReconcileParams struct {
	meta                      metadata.Metadata
	policyConfig              PolicyConfig
	basePath                  string
	setDefaultSecurityContext bool
	operatorNamespace         string
	existingPods              []corev1.Pod
	scaleToZero               bool
}

type poolReconcileResult struct {
	selectorMismatch bool
	requeue          bool
}

func (d *driver) reconcilePools(
	ctx context.Context,
	state *State,
	kb *kbv1.Kibana,
	plans []poolPlan,
	params poolReconcileParams,
) (poolReconcileResult, error) {
	var (
		aggregateAvailable int32
		aggregateCount     int32
		aggregateHealth    = commonv1.GreenHealth
	)
	reconciledSecrets := map[string]bool{}

	for _, plan := range plans {
		poolStatus, transition, err := d.reconcilePool(ctx, kb, plan, params, reconciledSecrets, state.Kibana.Status.DeploymentStatus)
		if err != nil {
			return poolReconcileResult{}, err
		}
		if transition.selectorMismatch {
			return transition, nil
		}

		aggregateAvailable += poolStatus.AvailableNodes
		aggregateCount += poolStatus.Count
		if poolStatus.Health != commonv1.GreenHealth {
			aggregateHealth = poolStatus.Health
		}
		buildStatus(state, kb, plan.role, poolStatus, aggregateHealth, aggregateAvailable, aggregateCount)
	}

	return poolReconcileResult{}, nil
}

func (d *driver) reconcilePool(
	ctx context.Context,
	kb *kbv1.Kibana,
	plan poolPlan,
	params poolReconcileParams,
	reconciledSecrets map[string]bool,
	previousStatus commonv1.DeploymentStatus,
) (commonv1.DeploymentStatus, poolReconcileResult, error) {
	poolConfig, err := NewConfigSettings(ctx, d.client, *kb, plan.config, d.version, d.ipFamily, params.policyConfig.KibanaConfig)
	if err != nil {
		return commonv1.DeploymentStatus{}, poolReconcileResult{}, err
	}

	var existingDeployment appsv1.Deployment
	var existingSelector map[string]string
	if err := d.client.Get(ctx, types.NamespacedName{Namespace: kb.Namespace, Name: plan.deploymentName}, &existingDeployment); err == nil {
		if existingDeployment.Spec.Selector != nil {
			existingSelector = existingDeployment.Spec.Selector.MatchLabels
		}
	} else if !apierrors.IsNotFound(err) {
		return commonv1.DeploymentStatus{}, poolReconcileResult{}, err
	}

	selector := d.deploymentSelector(kb, plan.identityLabels, existingSelector)
	mismatch, err := d.handleDeploymentSelectorMismatch(ctx, kb, &existingDeployment, existingSelector, selector, plan.deploymentName)
	if err != nil {
		return commonv1.DeploymentStatus{}, poolReconcileResult{}, err
	}
	if mismatch.mismatch {
		return commonv1.DeploymentStatus{}, poolReconcileResult{
			selectorMismatch: true,
			requeue:          mismatch.requeue,
		}, nil
	}

	if !reconciledSecrets[plan.configSecretName] {
		if err := ReconcileConfigSecret(ctx, d.client, *kb, poolConfig, plan.configSecretName, params.meta); err != nil {
			return commonv1.DeploymentStatus{}, poolReconcileResult{}, err
		}
		reconciledSecrets[plan.configSecretName] = true
	}

	replicas, oldReplicas := d.deploymentReplicas(ulog.FromContext(ctx), params.scaleToZero, kb, plan, &existingDeployment)
	dpParams, err := d.deploymentParams(ctx, kb, deploymentBuildParams{
		plan:                      plan,
		replicas:                  replicas,
		replicasAnnotationValue:   oldReplicas,
		policyAnnotations:         params.policyConfig.PodAnnotations,
		basePath:                  params.basePath,
		setDefaultSecurityContext: params.setDefaultSecurityContext,
		operatorNamespace:         params.operatorNamespace,
		meta:                      params.meta,
		selector:                  selector,
	})
	if err != nil {
		return commonv1.DeploymentStatus{}, poolReconcileResult{}, err
	}

	expectedDp := deployment.New(dpParams)
	reconciledDp, err := common.ReconcilePauseAware(ctx, d.client, d.recorder, expectedDp, kb, deployment.Reconcile)
	if err != nil {
		return commonv1.DeploymentStatus{}, poolReconcileResult{}, err
	}

	poolStatus, err := common.DeploymentStatus(ctx, previousStatus, reconciledDp, params.existingPods, kblabel.KibanaVersionLabelName)
	if err != nil {
		return commonv1.DeploymentStatus{}, poolReconcileResult{}, err
	}
	return poolStatus, poolReconcileResult{}, nil
}

func buildStatus(
	state *State,
	kb *kbv1.Kibana,
	role kblabel.Role,
	poolStatus commonv1.DeploymentStatus,
	aggregateHealth commonv1.DeploymentHealth,
	aggregateAvailable,
	aggregateCount int32,
) {
	if kb.BackgroundTasksEnabled() && state.Kibana.Status.Pools == nil {
		state.Kibana.Status.Pools = &kbv1.KibanaPoolsStatuses{}
	}

	switch {
	case role.IsSinglePool():
		state.Kibana.Status.DeploymentStatus = poolStatus
	case role.IsPrimary():
		state.Kibana.Status.DeploymentStatus = poolStatus
		state.Kibana.Status.Pools.Primary = poolStatusForAPI(poolStatus)
	case role.IsBackgroundTasks():
		state.Kibana.Status.Pools.BackgroundTasks = poolStatusForAPI(poolStatus)
	}

	if !role.IsSinglePool() {
		state.Kibana.Status.DeploymentStatus.Health = aggregateHealth
		state.Kibana.Status.DeploymentStatus.AvailableNodes = aggregateAvailable
		state.Kibana.Status.DeploymentStatus.Count = aggregateCount
		state.Kibana.Status.DeploymentStatus.Selector = metav1.FormatLabelSelector(&metav1.LabelSelector{
			MatchLabels: kb.GetIdentityLabels(),
		})
	} else {
		state.Kibana.Status.Pools = nil
	}
}

func poolStatusForAPI(status commonv1.DeploymentStatus) *kbv1.KibanaPoolStatus {
	return &kbv1.KibanaPoolStatus{
		Selector:       status.Selector,
		Count:          status.Count,
		AvailableNodes: status.AvailableNodes,
		Health:         status.Health,
	}
}
