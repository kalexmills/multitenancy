##
# Makefile targets for CI and local development.
#

THISFILE := $(realpath $(firstword $(MAKEFILE_LIST)))
PWD := $(strip $(realpath $(dir $(THISFILE))))
LOCALBIN := $(PWD)/.bin
OUTDIR := $(PWD)/.out
.DEFAULT_GOAL := help

$(LOCALBIN):
	mkdir -p $(LOCALBIN)

$(OUTDIR):
	mkdir -p $(OUTDIR)

##
# help
# Displays a (hopefully) useful help screen to the user
#
# NOTE: Keep 'help' as first target in case .DEFAULT_GOAL is not honored
# NOTE: ONLY targets with ## comments are shown
#
.PHONY: help
help: ## Shows this help screen.
	@echo
	@echo  "Make targets:"
	@echo
	@cat $(THISFILE)* | \
		sed -n -E 's/^([^.][^: \?\=]+)\s*:(([^=#]*##\s*(.*[^[:space:]])\s*)|[^=].*)$$/    \1	\4/p' | \
		expand -t15
	@echo

##
# Make targets for binary build
#

.PHONY: lint
lint: ## Runs go fmt.
	@golangci-lint run

.PHONY: test
test: ## Runs go test.
	@CGO_ENABLED=1 GOTRACEBACK=all go test -failfast -race -v ./...

.PHONY: bench
bench: ## Runs go test with benchmarks.
	@go test -bench=. -run=^$$ ./...

.PHONY: generate
generate: .generate-code .generate-crds ## Generates code and CRDs

.PHONY: .generate-code
.generate-code:
	@./hack/update-codegen.sh

.PHONY: .generate-crds
.generate-crds: $(OUTDIR)
	@controller-gen rbac:roleName=multitenancy-role crd paths=./... \
 		output:dir=.out/k8sresources
	@mkdir -p charts/multitenancy/templates/generated
	@cp ./.out/k8sresources/* charts/multitenancy/templates/generated

GOOS?=linux

.PHONY: build
build: $(PWD)/.out ## Builds the multitenancy binary.
	@CGO_ENABLED=0 GOOS=$(GOOS) go build -o .out/controller cmd/main.go

##
# Make targets for container build
#

IMG_REPO?=docker.io/kalexmills/multitenancy
IMG_TAG?=latest
IMG?=$(IMG_REPO):$(IMG_TAG)

.PHONY: docker-build
docker-build: build ## Rebuilds binary and builds the docker image.
	@docker build -t ${IMG} .

##
# Make targets for kind cluster
#

CLUSTER_NAME?=multitenancy-test

.PHONY: kind-create
kind-create: ## Create a new kind cluster
	@kind create cluster

.PHONY: kind-load
kind-load: ## Load the image to kind cluster.
	@kind load docker-image $(IMG)

.PHONY: kind-refresh
kind-refresh: ## Builds and loads a timestamped image into the kind cluster.
	@IMG_TAG=$$(date +%s) $(MAKE) generate docker-build kind-load helm-install

.PHONY: kind-delete
kind-delete: ## Delete kind cluster
	@kind delete cluster

NAMESPACE?=multitenancy
HELM_RELEASE_NAME?=multitenancy

.PHONY: helm-install
helm-install: ## Install helm chart using current cluster context.
	@helm upgrade --install $(HELM_RELEASE_NAME) charts/multitenancy -n $(NAMESPACE) --create-namespace --set image.tag=$(IMG_TAG)

.PHONY: helm-uninstall
helm-uninstall: ## Uninstall helm chart.
	@helm uninstall $(HELM_RELEASE_NAME) -n $(NAMESPACE)

