// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package license

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	esclient "github.com/elastic/cloud-on-k8s/operators/pkg/controller/elasticsearch/client"
	"github.com/elastic/cloud-on-k8s/operators/pkg/controller/elasticsearch/name"
	"github.com/elastic/cloud-on-k8s/operators/pkg/utils/k8s"
	pkgerrors "github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

func applyLinkedLicense(
	c k8s.Client,
	esCluster types.NamespacedName,
	updater func(license esclient.License) error,
) error {
	var license corev1.Secret
	// the underlying assumption here is that either a user or a
	// license controller has created a cluster license in the
	// namespace of this cluster following the cluster-license naming
	// convention
	err := c.Get(
		types.NamespacedName{
			Namespace: esCluster.Namespace,
			Name:      name.LicenseSecretName(esCluster.Name),
		},
		&license,
	)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// no license linked to this cluster. Expected for clusters running on basic or trial.
			return nil
		}
		return err
	}
	if len(license.Data) == 0 {
		return errors.New("empty license linked to this cluster")
	}
	for _, v := range license.Data {
		var lic esclient.License
		err = json.Unmarshal(v, &lic)
		if err == nil {
			return updater(lic)
		}
	}
	return pkgerrors.Wrap(err, "No valid license found in license secret")
}

// updateLicense make the call to Elasticsearch to set the license. This function exists mainly to facilitate testing.
func updateLicense(
	c esclient.Client,
	current *esclient.License,
	desired esclient.License,
) error {
	if current != nil && current.UID == desired.UID {
		return nil // we are done already applied
	}
	request := esclient.LicenseUpdateRequest{
		Licenses: []esclient.License{
			desired,
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), esclient.DefaultReqTimeout)
	defer cancel()
	response, err := c.UpdateLicense(ctx, request)
	if err != nil {
		return err
	}
	if !response.IsSuccess() {
		return fmt.Errorf("failed to apply license: %s", response.LicenseStatus)
	}
	return nil
}
