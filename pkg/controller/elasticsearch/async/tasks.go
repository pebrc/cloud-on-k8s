// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package async

import (
	"context"

	"github.com/elastic/cloud-on-k8s/pkg/controller/common/async"
	esclient "github.com/elastic/cloud-on-k8s/pkg/controller/elasticsearch/client"
	"github.com/elastic/cloud-on-k8s/pkg/utils/stringsutil"
	"k8s.io/apimachinery/pkg/types"
)

type NodesTask struct {
	esClient esclient.Client
	cluster  types.NamespacedName
}

func NewNodesTask(es types.NamespacedName, client esclient.Client) *NodesTask {
	return &NodesTask{
		esClient: client,
		cluster:  es,
	}
}

func (n *NodesTask) Key() string {
	return stringsutil.Concat(n.cluster.Namespace, "/", n.cluster.Name, "/nodes")
}

func (n *NodesTask) Run() (interface{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), esclient.DefaultReqTimeout)
	defer cancel()
	nodes, err := n.esClient.GetNodes(ctx)
	if err != nil {
		return nil, err
	}
	return nodes.Names(), nil
}

func (n *NodesTask) NodeNames(tm async.TaskManager, asOf async.LogicalTime) ([]string, error, bool) {
	result, err, ready := tm.ConsumeResult(n, asOf)
	return result.([]string), err, ready
}

var _ async.Task = &NodesTask{}
