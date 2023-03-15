// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package nodespec

import (
	"bytes"
	"path"
	"path/filepath"
	"text/template"

	v1 "k8s.io/api/core/v1"

	"github.com/elastic/cloud-on-k8s/v2/pkg/controller/elasticsearch/label"
	"github.com/elastic/cloud-on-k8s/v2/pkg/controller/elasticsearch/user"
	"github.com/elastic/cloud-on-k8s/v2/pkg/controller/elasticsearch/volume"
)

func NewPreStopHook() *v1.LifecycleHandler {
	return &v1.LifecycleHandler{
		Exec: &v1.ExecAction{
			Command: []string{"bash", "-c", path.Join(volume.ScriptsVolumeMountPath, PreStopHookScriptConfigKey)}},
	}
}

/*
  - use probe user:
 	- need to elevate permissions
	- if permission elevations fails because ES not operational: status quo
	- maintenance issue: misleading user name/pw path
  - add new user
	- somewhat unnecessary, does not improve security posture
	- can use meaningful name
	- probably best compromise
  - rename probe user and extend permissions
	- transition issue: readiness probe will fail for existing nodes: this is a blocker
*/

const PreStopHookScriptConfigKey = "pre-stop-hook-script.sh"

var preStopHookScriptTemplate = template.Must(template.New("pre-stop").Parse(`#!/usr/bin/env bash

set -euo pipefail

# This script will wait for up to $PRE_STOP_ADDITIONAL_WAIT_SECONDS before allowing termination of the Pod 
# This slows down the process shutdown and allows to make changes to the pool gracefully, without blackholing traffic when DNS
# still contains the IP that is already inactive. 
# As this runs in parallel to grace period after which process is SIGKILLed,
# it should be set to allow enough time for the process to gracefully terminate.
# It allows kube-proxy to refresh its rules and remove the terminating Pod IP.
# Kube-proxy refresh period defaults to every 30 seconds, but the operation itself can take much longer if
# using iptables with a lot of services, in which case the default 30sec might not be enough.
# Also gives some additional bonus time to in-flight requests to terminate, and new requests to still
# target the Pod IP before Elasticsearch stops.
PRE_STOP_ADDITIONAL_WAIT_SECONDS=${PRE_STOP_ADDITIONAL_WAIT_SECONDS:=50}

# capture response bodies in a temp file for better error messages and to extract necessary information for subsequent requests
resp_body=$(mktemp)
trap "rm -f $resp_body" EXIT

script_start=$(date +%s)

# compute time in seconds since the given start time
function duration() {
	local start=$1
	end=$(date +%s)
	echo $((end-start))
}

# use DNS errors as a proxy to abort this script early if there is no chance of successful completion
# DNS errors are for example expected when the whole cluster including its service is being deleted
# and the service URL can no longer be resolved even though we still have running Pods.
max_dns_errors=2
global_dns_error_cnt=0

function request() {
	local status exit
    status=$(curl -k -sS -o $resp_body -w "%{http_code}" "$@")
    exit=$?
    if [ "$exit" -ne 0 ] || [ "$status" -lt 200 ] || [ "$status" -gt 299 ]; then
		# track curl DNS errors separately
		if [ "$exit" -eq 6 ]; then ((global_dns_error_cnt++)); fi
        echo  $status $resp_body
        return $exit
    fi
    global_dns_error_cnt=0
    return 0
}

function retry() {
  local retries=$1
  shift

  local count=0
  until "$@"; do
    exit=$?
    wait=$((2 ** count))
    count=$((count + 1))
	if [ $global_dns_error_cnt -gt $max_dns_errors ]; then
		error_exit "too many DNS errors, giving up"
    fi
    if [ $count -lt "$retries" ]; then
      printf "Retry %s/%s exited %s, retrying in %s seconds...\n" "$count" "$retries" "$exit" "$wait" >&2
      sleep $wait
    else
      printf "Retry %s/%s exited %s, no more retries left.\n" "$count" "$retries" "$exit" >&2
      return $exit
    fi
  done
  return 0
}

function error_exit() {
  echo $1 1>&2
  exit 1
}

function delayed_exit() {
	local elapsed=$(duration $script_start)
    sleep $(($PRE_STOP_ADDITIONAL_WAIT_SECONDS - $elapsed))
	exit 0
}

function is_master(){
  labels="{{.LabelsFile}}"
  grep 'master="true"' $labels
}

function supports_node_shutdown() {
	local version="$1"
	version=${version#[vV]}
	major="${version%%\.*}"
	minor="${version#*.}"
	minor="${minor%.*}"
	patch="${version##*.}"
	# node shutdown is supported as of 7.15.2
	if [ "$major" -lt 7 ]  || ([ "$major" -eq 7 ] && [ "$minor" -eq 15 ] && [ "$patch" -lt 2 ]); then 
		return 1
	fi
	return 0
}

version=""
if [[ -f "{{.LabelsFile}}" ]]; then
  # get Elasticsearch version from the downward API
  version=$(grep "{{.VersionLabelName}}" {{.LabelsFile}} | cut -d '=' -f 2)
  # remove quotes
  version=$(echo "${version}" | tr -d '"')
fi

# if ES version does not support node shutdown exit early TODO bash regex 
if ! supports_node_shutdown $version; then
  delayed_exit 
fi

# setup basic auth if credentials are available TODO dedicated user?
if [ -f "{{.PreStopUserPasswordPath}}" ]; then
  PROBE_PASSWORD=$(<{{.PreStopUserPasswordPath}})
  BASIC_AUTH="-u {{.PreStopUserName}}:${PROBE_PASSWORD}"
else
  BASIC_AUTH=''
fi

ES_URL={{.ServiceURL}}

if is_master; then
  retry 10 request -X POST "$ES_URL/_cluster/voting_config_exclusions?node_names=$POD_NAME" $BASIC_AUTH
  # we ignore the error here and try to call at least node shutdown
fi

echo "retrieving node ID"
retry 10 request -X GET "$ES_URL/_cat/nodes?full_id=true&h=id,name" $BASIC_AUTH
if [ "$?"  -ne 0 ]; then
	error_exit $status
fi

NODE_ID=$(grep $POD_NAME $resp_body | cut -f 1 -d ' ')

# check if there is an ongoing shutdown request
request -X GET $ES_URL/_nodes/$NODE_ID/shutdown $BASIC_AUTH
if grep -q -v '"nodes":\[\]' $resp_body; then
	delayed_exit      
fi

echo "initiating node shutdown"
retry 10 request -X PUT $ES_URL/_nodes/$NODE_ID/shutdown $BASIC_AUTH -H 'Content-Type: application/json' -d'
{
  "type": "restart", 
  "reason": "pre-stop hook"
}
'
if [ "$?" -ne 0 ]; then
   error_exit "Failed to call node shutdown API" $resp_body
fi

while :
do 
   echo "waiting for node shutdown to complete"
   request -X GET $ES_URL/_nodes/$NODE_ID/shutdown $BASIC_AUTH
   if [ "$?" -ne 0 ]; then
      continue
   fi
   if grep -q -v 'IN_PROGRESS\|STALLED' $resp_body; then 
      break
   fi
   sleep 10 
done

delayed_exit
`))

func RenderPreStopHookScript(svcURL string) (string, error) {
	vars := map[string]string{
		"PreStopUserName":         user.PreStopUserName,
		"PreStopUserPasswordPath": filepath.Join(volume.PodMountedUsersSecretMountPath, user.PreStopUserName),
		// edge case: protocol change (http/https) combined with external node shutdown might not work out well due to
		// script propagation delays. But it is not a legitimate production use case as users are not expected to change
		// protocol on production systems
		"ServiceURL":       svcURL,
		"LabelsFile":       filepath.Join(volume.DownwardAPIMountPath, volume.LabelsFile),
		"VersionLabelName": label.VersionLabelName,
	}
	var script bytes.Buffer
	err := preStopHookScriptTemplate.Execute(&script, vars)
	return script.String(), err
}
