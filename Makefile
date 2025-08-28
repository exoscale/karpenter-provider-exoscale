GO_MK_REF := v2.0.3

# make go.mk a dependency for all targets
.EXTRA_PREREQS = go.mk

ifndef MAKE_RESTARTS
# This section will be processed the first time that make reads this file.

# This causes make to re-read the Makefile and all included
# makefiles after go.mk has been cloned.
Makefile:
	@touch Makefile
endif

.PHONY: go.mk
.ONESHELL:
go.mk:
	@if [ ! -d "go.mk" ]; then
		git clone https://github.com/exoscale/go.mk.git
	fi
	@cd go.mk
	@if ! git show-ref --quiet --verify "refs/heads/${GO_MK_REF}"; then
		git fetch
	fi
	@if ! git show-ref --quiet --verify "refs/tags/${GO_MK_REF}"; then
		git fetch --tags
	fi
	git checkout --quiet ${GO_MK_REF}

all: installcrds

# Run tests
generate:
	controller-gen object:headerFile=./hack/boilerplate.go.txt paths="./..."

manifests:
	controller-gen crd:crdVersions=v1 rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases

installcrds:
	kustomize build config/crd/bases | kubectl apply -f -

install: manifests
	kustomize build config/base | kubectl apply -f -
	
## Project

PACKAGE := github.com/exoscale/karpenter-exoscale
PROJECT_URL := https://$(PACKAGE)
GO_MAIN_PKG_PATH := ./cmd/karpenter-exoscale

EXTRA_ARGS := -parallel 3 -count=1 -failfast

# Dependencies

# Requires: https://github.com/exoscale/go.mk
# - install: git submodule update --init --recursive go.mk
# - update:  git submodule update --remote
go.mk/init.mk:
include go.mk/init.mk
go.mk/public.mk:
include go.mk/public.mk

## Targets

# Docker
include Makefile.docker