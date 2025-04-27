#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
CODEGEN_PKG=./.bin

source "${CODEGEN_PKG}/kube_codegen.sh"

THIS_PKG="k8s.io/sample-controller"

kube::codegen::gen_helpers \
    --boilerplate "${SCRIPT_ROOT}/hack/boilerplate.go.txt" \
    "${SCRIPT_ROOT}/pkg/apis"

kube::codegen::gen_client \
    --with-watch \
    --output-dir "${SCRIPT_ROOT}/pkg/apis/generated" \
    --output-pkg "${THIS_PKG}/pkg/apis/generated" \
    --boilerplate "${SCRIPT_ROOT}/hack/boilerplate.go.txt" \
    "${SCRIPT_ROOT}/pkg/apis"

kube::codegen::gen_register \
    --boilerplate "${SCRIPT_ROOT}/hack/boilerplate.go.txt" \
        "${SCRIPT_ROOT}/pkg/apis"