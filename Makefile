all: installcrds

# Run tests
test:
	go test ./... -coverprofile cover.out

generate:
	controller-gen object:headerFile=./hack/boilerplate.go.txt paths="./..."

manifests:
	controller-gen crd:crdVersions=v1 rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases

installcrds:
	kustomize build config/crd/bases | kubectl apply -f -

install: manifests
	kustomize build config/base | kubectl apply -f -