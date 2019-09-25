// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package v1alpha1

import (
	"encoding/json"

	ucfg "github.com/elastic/go-ucfg"
)

// CfgOptions are config options for YAML config. Currently contains only support for dotted keys.
var CfgOptions = []ucfg.Option{ucfg.PathSep(".")}

// Config represents untyped YAML configuration inside a spec.
type Config json.RawMessage
