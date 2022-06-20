// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package webhook

import (
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/elastic/cloud-on-k8s/pkg/controller/common/license"
)

func (v *validatingWebhook) commonValidations(req admission.Request, obj runtime.Object) error {
	errorList := hasRequestedLicenseLevel(obj, v.licenseChecker)
	if len(errorList) > 0 {
		return apierrors.NewInvalid(schema.GroupKind{
			Group: req.Kind.Group,
			Kind:  req.Kind.Kind,
		}, req.Name, errorList)
	}
	return nil
}

func hasRequestedLicenseLevel(obj runtime.Object, checker license.Checker) field.ErrorList {
	accessor := meta.NewAccessor()
	annotations, err := accessor.Annotations(obj)
	if err != nil {
		whlog.Error(err, "while accessing runtime object metadata")
		return nil
	}
	var errs field.ErrorList
	ok, err := license.HasRequestedLicenseLevel(annotations, checker)
	if err != nil {
		whlog.Error(err, "while checking license level during validation")
		return nil
	}
	if !ok {
		errs = append(errs, field.Invalid(field.NewPath("metadata").Child("annotations").Child(license.Annotation), "enterprise", "Enterprise license required but ECK operator is running on a Basic license"))
	}
	return errs
}
