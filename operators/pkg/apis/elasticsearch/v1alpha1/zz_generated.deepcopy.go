// +build !ignore_autogenerated

// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

// Code generated by main. DO NOT EDIT.

package v1alpha1

import (
	commonv1alpha1 "github.com/elastic/cloud-on-k8s/operators/pkg/apis/common/v1alpha1"
	v1 "k8s.io/api/core/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ChangeBudget) DeepCopyInto(out *ChangeBudget) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ChangeBudget.
func (in *ChangeBudget) DeepCopy() *ChangeBudget {
	if in == nil {
		return nil
	}
	out := new(ChangeBudget)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ClusterSettings) DeepCopyInto(out *ClusterSettings) {
	*out = *in
	if in.InitialMasterNodes != nil {
		in, out := &in.InitialMasterNodes, &out.InitialMasterNodes
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ClusterSettings.
func (in *ClusterSettings) DeepCopy() *ClusterSettings {
	if in == nil {
		return nil
	}
	out := new(ClusterSettings)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Elasticsearch) DeepCopyInto(out *Elasticsearch) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Elasticsearch.
func (in *Elasticsearch) DeepCopy() *Elasticsearch {
	if in == nil {
		return nil
	}
	out := new(Elasticsearch)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Elasticsearch) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ElasticsearchList) DeepCopyInto(out *ElasticsearchList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	out.ListMeta = in.ListMeta
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Elasticsearch, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ElasticsearchList.
func (in *ElasticsearchList) DeepCopy() *ElasticsearchList {
	if in == nil {
		return nil
	}
	out := new(ElasticsearchList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ElasticsearchList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ElasticsearchSettings) DeepCopyInto(out *ElasticsearchSettings) {
	*out = *in
	out.Node = in.Node
	in.Cluster.DeepCopyInto(&out.Cluster)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ElasticsearchSettings.
func (in *ElasticsearchSettings) DeepCopy() *ElasticsearchSettings {
	if in == nil {
		return nil
	}
	out := new(ElasticsearchSettings)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ElasticsearchSpec) DeepCopyInto(out *ElasticsearchSpec) {
	*out = *in
	if in.SetVMMaxMapCount != nil {
		in, out := &in.SetVMMaxMapCount, &out.SetVMMaxMapCount
		*out = new(bool)
		**out = **in
	}
	in.HTTP.DeepCopyInto(&out.HTTP)
	if in.Nodes != nil {
		in, out := &in.Nodes, &out.Nodes
		*out = make([]NodeSpec, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.FeatureFlags != nil {
		in, out := &in.FeatureFlags, &out.FeatureFlags
		*out = make(commonv1alpha1.FeatureFlags, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	in.UpdateStrategy.DeepCopyInto(&out.UpdateStrategy)
	if in.PodDisruptionBudget != nil {
		in, out := &in.PodDisruptionBudget, &out.PodDisruptionBudget
		*out = new(commonv1alpha1.PodDisruptionBudgetTemplate)
		(*in).DeepCopyInto(*out)
	}
	if in.SecureSettings != nil {
		in, out := &in.SecureSettings, &out.SecureSettings
		*out = new(commonv1alpha1.SecretRef)
		**out = **in
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ElasticsearchSpec.
func (in *ElasticsearchSpec) DeepCopy() *ElasticsearchSpec {
	if in == nil {
		return nil
	}
	out := new(ElasticsearchSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ElasticsearchStatus) DeepCopyInto(out *ElasticsearchStatus) {
	*out = *in
	out.ReconcilerStatus = in.ReconcilerStatus
	out.ZenDiscovery = in.ZenDiscovery
	if in.RemoteClusters != nil {
		in, out := &in.RemoteClusters, &out.RemoteClusters
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ElasticsearchStatus.
func (in *ElasticsearchStatus) DeepCopy() *ElasticsearchStatus {
	if in == nil {
		return nil
	}
	out := new(ElasticsearchStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GroupingDefinition) DeepCopyInto(out *GroupingDefinition) {
	*out = *in
	in.Selector.DeepCopyInto(&out.Selector)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GroupingDefinition.
func (in *GroupingDefinition) DeepCopy() *GroupingDefinition {
	if in == nil {
		return nil
	}
	out := new(GroupingDefinition)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Node) DeepCopyInto(out *Node) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Node.
func (in *Node) DeepCopy() *Node {
	if in == nil {
		return nil
	}
	out := new(Node)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NodeSpec) DeepCopyInto(out *NodeSpec) {
	*out = *in
	if in.Config != nil {
		in, out := &in.Config, &out.Config
		*out = (*in).DeepCopy()
	}
	in.PodTemplate.DeepCopyInto(&out.PodTemplate)
	if in.VolumeClaimTemplates != nil {
		in, out := &in.VolumeClaimTemplates, &out.VolumeClaimTemplates
		*out = make([]v1.PersistentVolumeClaim, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NodeSpec.
func (in *NodeSpec) DeepCopy() *NodeSpec {
	if in == nil {
		return nil
	}
	out := new(NodeSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *UpdateStrategy) DeepCopyInto(out *UpdateStrategy) {
	*out = *in
	if in.Groups != nil {
		in, out := &in.Groups, &out.Groups
		*out = make([]GroupingDefinition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.ChangeBudget != nil {
		in, out := &in.ChangeBudget, &out.ChangeBudget
		*out = new(ChangeBudget)
		**out = **in
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new UpdateStrategy.
func (in *UpdateStrategy) DeepCopy() *UpdateStrategy {
	if in == nil {
		return nil
	}
	out := new(UpdateStrategy)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ZenDiscoveryStatus) DeepCopyInto(out *ZenDiscoveryStatus) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ZenDiscoveryStatus.
func (in *ZenDiscoveryStatus) DeepCopy() *ZenDiscoveryStatus {
	if in == nil {
		return nil
	}
	out := new(ZenDiscoveryStatus)
	in.DeepCopyInto(out)
	return out
}
