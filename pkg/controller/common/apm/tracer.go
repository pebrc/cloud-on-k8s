// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package apm

import (
	"github.com/elastic/cloud-on-k8s/pkg/about"
	"github.com/go-logr/logr"
	"go.elastic.co/apm"
)

func NewTracer(serviceName string, log logr.Logger) *apm.Tracer {
	build := about.GetBuildInfo()
	tracer, err := apm.NewTracer(serviceName, build.Version+"-"+build.Hash)
	if err != nil {
		// don't fail the application because tracing fails
		log.Error(err, "failed to created tracer for "+serviceName)
	}
	tracer.SetLogger(NewLogAdapter(log))
	return tracer
}
