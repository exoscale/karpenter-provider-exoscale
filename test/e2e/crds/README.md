# E2E Test CRDs

This directory contains the CRDs required for e2e tests to deploy Karpenter to the test SKS cluster.

## Files

- `karpenter.sh_nodeclaims.yaml` - Karpenter NodeClaim CRD (from sigs.k8s.io/karpenter@v1.8.0)
- `karpenter.sh_nodepools.yaml` - Karpenter NodePool CRD (from sigs.k8s.io/karpenter@v1.8.0)
- `karpenter.exoscale.com_exoscalenodeclasses.yaml` - ExoscaleNodeClass CRD (from config/crd/bases/)

## Updating CRDs

When updating the Karpenter version in go.mod, update these CRDs:

```bash
# Update Karpenter CRDs
KARPENTER_VERSION=$(go list -m -f '{{.Version}}' sigs.k8s.io/karpenter)
KARPENTER_PATH=$(go list -m -f '{{.Dir}}' sigs.k8s.io/karpenter)
cp $KARPENTER_PATH/pkg/apis/crds/karpenter.sh_nodeclaims.yaml test/e2e/crds/
cp $KARPENTER_PATH/pkg/apis/crds/karpenter.sh_nodepools.yaml test/e2e/crds/

# Update ExoscaleNodeClass CRD
make manifests
cp config/crd/bases/karpenter.exoscale.com_exoscalenodeclasses.yaml test/e2e/crds/
```
