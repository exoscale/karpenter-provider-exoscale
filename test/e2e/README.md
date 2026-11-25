# e2e tests

Each run creates a disposable SKS cluster without the karpenter addon, builds
Karpenter from this checkout and runs it next to the tests, with the identity
SKS uses in production: kubeconfig user `karpenter-<cluster-id>` bound to the
generated `config/rbac/role.yaml`, and an API key restricted to the policy in
[doc/iam.md](../../doc/iam.md).

CI runs the suite on every pull request, once per Kubernetes version SKS
supports (`.github/workflows/e2e.yml`).

## Running locally

```bash
cd test/e2e
export EXOSCALE_API_KEY=... EXOSCALE_API_SECRET=...  # needs SKS, compute and IAM rights
export K8S_VERSION=<version>                         # optional, defaults to the latest (go run ./cmd/versions)
go test -v -count=1 -timeout 80m . -args -ginkgo.v
```

Optional: `EXOSCALE_ZONE` (default `ch-gva-2`), `EXOSCALE_API_ENDPOINT`,
`E2E_NAME` (resource name, must start with `karpenter-e2e-`), `KARPENTER_LOG`.

The suite destroys what it created. If a run is interrupted:

```bash
go run ./cmd/cleanup -name <E2E_NAME>   # one run
go run ./cmd/cleanup -older-than 3h     # every leftover
```
