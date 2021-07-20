// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package migration

import (
	"context"
	"strings"

	esv1 "github.com/elastic/cloud-on-k8s/pkg/apis/elasticsearch/v1"
	esclient "github.com/elastic/cloud-on-k8s/pkg/controller/elasticsearch/client"
	"github.com/elastic/cloud-on-k8s/pkg/controller/elasticsearch/shutdown"
	ulog "github.com/elastic/cloud-on-k8s/pkg/utils/log"
)

var log = ulog.Log.WithName("migrate-data")

type ShardMigration struct {
	es esv1.Elasticsearch
	c  esclient.Client
	s  esclient.ShardLister
}

var _ shutdown.Interface = &ShardMigration{}

func NewShardMigration(es esv1.Elasticsearch, c esclient.Client, s esclient.ShardLister) shutdown.Interface {
	return &ShardMigration{
		es: es,
		c:  c,
		s:  s,
	}
}

func (sm *ShardMigration) ReconcileShutdowns(ctx context.Context, leavingNodes []string) error {
	return migrateData(ctx, sm.es, sm.c, leavingNodes)
}

func (sm *ShardMigration) ShutdownStatus(ctx context.Context, podName string) (shutdown.NodeShutdownStatus, error) {
	migrating, err := nodeMayHaveShard(ctx, sm.es, sm.s, podName)
	if err != nil {
		return shutdown.NodeShutdownStatus{}, err
	}
	if migrating {
		return shutdown.NodeShutdownStatus{Status: esclient.ShutdownStarted}, nil
	}
	return shutdown.NodeShutdownStatus{Status: esclient.ShutdownComplete}, nil
}

// nodeMayHaveShard returns true if one of those condition is met:
// - the given ES Pod is holding at least one shard (primary or replica)
// - some shards in the cluster don't have a node assigned, in which case we can't be sure about the 1st condition
//   this may happen if the node was just restarted: the shards it is holding appear unassigned
func nodeMayHaveShard(ctx context.Context, es esv1.Elasticsearch, shardLister esclient.ShardLister, podName string) (bool, error) {
	shards, err := shardLister.GetShards(ctx)
	if err != nil {
		return false, err
	}
	for _, shard := range shards {
		// shard still on the node
		if shard.NodeName == podName {
			return true, nil
		}
		// shard node undefined (likely unassigned)
		if shard.NodeName == "" {
			log.Info("Found orphan shard, preventing data migration",
				"namespace", es.Namespace, "es_name", es.Name,
				"index", shard.Index, "shard", shard.Shard, "shard_state", shard.State)
			return true, nil
		}
	}
	return false, nil
}

// migrateData sets allocation filters for the given nodes.
func migrateData(
	ctx context.Context,
	es esv1.Elasticsearch,
	allocationSetter esclient.AllocationSetter,
	leavingNodes []string,
) error {
	// compute the expected exclusion value
	exclusions := "none_excluded"
	if len(leavingNodes) > 0 {
		exclusions = strings.Join(leavingNodes, ",")
	}
	log.Info("Setting routing allocation excludes", "namespace", es.Namespace, "es_name", es.Name, "value", exclusions)
	return allocationSetter.ExcludeFromShardAllocation(ctx, exclusions)
}
