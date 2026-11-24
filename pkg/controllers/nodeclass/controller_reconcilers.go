package nodeclass

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"sort"
	"strings"

	egov3 "github.com/exoscale/egoscale/v3"
	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/providers/userdata"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type hashEntry struct {
	key   string
	value []byte
}

func hashEntries(entries []hashEntry) string {
	if len(entries) == 0 {
		return ""
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].key < entries[j].key })
	h := sha256.New()
	for _, e := range entries {
		_, _ = h.Write([]byte(e.key))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(e.value)
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (r *ExoscaleNodeClassReconciler) reconcileTemplate(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) error {
	t, err := r.TemplateResolver.ResolveTemplate(ctx, nodeClass)
	if err != nil {
		return fmt.Errorf("failed to resolve template ID: %w", err)
	}

	if _, err := r.ExoscaleClient.GetTemplate(ctx, egov3.UUID(t.ID)); err != nil {
		return fmt.Errorf("template %s not found or not accessible: %w", t.ID, err)
	}

	nodeClass.Status.ImageID = t.ID
	return nil
}

// resolveIDs resolves the deprecated ID list and the selector terms of one
// Exoscale resource kind into resource IDs.
func resolveIDs[T any](
	ctx context.Context,
	kind string,
	ids []string,
	terms []apiv1.SelectorTerms,
	getByID func(context.Context, egov3.UUID) (*T, error),
	getByName func(context.Context, string) (*T, error),
	idOf func(*T) egov3.UUID,
) ([]string, error) {
	resolved := []string{}
	for _, id := range ids {
		obj, err := getByID(ctx, egov3.UUID(id))
		if err != nil {
			return nil, fmt.Errorf("failed to get %s %s: %w", kind, id, err)
		}
		resolved = append(resolved, idOf(obj).String())
	}

	for _, term := range terms {
		var obj *T
		var err error
		switch {
		case term.ID != "":
			obj, err = getByID(ctx, egov3.UUID(term.ID))
		case term.Name != "":
			obj, err = getByName(ctx, term.Name)
			if err != nil {
				return nil, fmt.Errorf("failed to get %s by name: %w", kind, err)
			}
			if obj == nil {
				err = fmt.Errorf("%s with name %s not found", kind, term.Name)
			}
		default:
			err = fmt.Errorf("selector requires id or name")
		}
		if err != nil {
			return nil, fmt.Errorf("failed to get %s for selector %+v: %w", kind, term, err)
		}
		resolved = append(resolved, idOf(obj).String())
	}
	return resolved, nil
}

func (r *ExoscaleNodeClassReconciler) reconcileSecurityGroups(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) error {
	ids, err := resolveIDs(ctx, "security group", nodeClass.Spec.SecurityGroups, nodeClass.Spec.SecurityGroupSelectorTerms,
		r.ExoscaleClient.GetSecurityGroup, r.getCachedSecurityGroupByName,
		func(sg *egov3.SecurityGroup) egov3.UUID { return sg.ID })
	if err != nil {
		return err
	}
	nodeClass.Status.SecurityGroups = ids
	return nil
}

func (r *ExoscaleNodeClassReconciler) reconcileAntiAffinityGroups(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) error {
	ids, err := resolveIDs(ctx, "anti-affinity group", nodeClass.Spec.AntiAffinityGroups, nodeClass.Spec.AntiAffinityGroupSelectorTerms,
		r.ExoscaleClient.GetAntiAffinityGroup, r.getCachedAntiAffinityGroupByName,
		func(aag *egov3.AntiAffinityGroup) egov3.UUID { return aag.ID })
	if err != nil {
		return err
	}
	nodeClass.Status.AntiAffinityGroups = ids
	return nil
}

// Elastic IPs have no name in the Exoscale API: the selector name is matched
// against the IP address.
func (r *ExoscaleNodeClassReconciler) reconcileElasticIPs(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) error {
	ids, err := resolveIDs(ctx, "elastic IP", nil, nodeClass.Spec.ElasticIPSelectorTerms,
		r.ExoscaleClient.GetElasticIP, r.getCachedElasticIPByAddress,
		func(eip *egov3.ElasticIP) egov3.UUID { return eip.ID })
	if err != nil {
		return err
	}
	nodeClass.Status.ElasticIPs = ids
	return nil
}

func (r *ExoscaleNodeClassReconciler) reconcilePrivateNetworks(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) error {
	if len(nodeClass.Spec.PrivateNetworks) > 0 || len(nodeClass.Spec.PrivateNetworkSelectorTerms) > 0 {
		log.FromContext(ctx).Info("privateNetworks/privateNetworkSelectorTerms are deprecated, use vpcSubnetSelectorTerms instead")
	}
	ids, err := resolveIDs(ctx, "private network", nodeClass.Spec.PrivateNetworks, nodeClass.Spec.PrivateNetworkSelectorTerms,
		r.ExoscaleClient.GetPrivateNetwork, r.getCachedPrivateNetworkByName,
		func(pn *egov3.PrivateNetwork) egov3.UUID { return pn.ID })
	if err != nil {
		return err
	}
	nodeClass.Status.PrivateNetworks = ids
	return nil
}

func (r *ExoscaleNodeClassReconciler) resolveVpc(ctx context.Context, selector *apiv1.SelectorTerms) (egov3.UUID, error) {
	if selector == nil {
		return "", fmt.Errorf("vpcSelector is required when vpcSubnetSelectorTerms is set")
	}

	id := egov3.UUID(selector.ID)
	if id == "" {
		if selector.Name == "" {
			return "", fmt.Errorf("vpcSelector requires id or name: selector %+v", selector)
		}
		entry, err := r.getCachedVpcByName(ctx, selector.Name)
		if err != nil {
			return "", fmt.Errorf("failed to get vpc by name: %w", err)
		}
		if entry == nil {
			return "", fmt.Errorf("vpc with name %s not found", selector.Name)
		}
		id = entry.ID
	}

	vpc, err := r.ExoscaleClient.GetVpc(ctx, id)
	if err != nil {
		return "", fmt.Errorf("failed to get vpc %s: %w", id, err)
	}
	return vpc.ID, nil
}

// reconcileVpcSubnets resolves spec.vpcSelector into status.vpcID and
// spec.vpcSubnetSelectorTerms into status.vpcSubnets (subnet IDs within that
// VPC). An instance can only be attached to subnets of a single VPC, so the
// VPC is selected explicitly and subnets are only looked up inside it.
func (r *ExoscaleNodeClassReconciler) reconcileVpcSubnets(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) error {
	subnetIDs := []string{}
	var vpcID egov3.UUID

	if len(nodeClass.Spec.VpcSubnetSelectorTerms) > 0 {
		var err error
		if vpcID, err = r.resolveVpc(ctx, nodeClass.Spec.VpcSelector); err != nil {
			return err
		}

		for _, selector := range nodeClass.Spec.VpcSubnetSelectorTerms {
			id := egov3.UUID(selector.ID)
			if id == "" {
				if selector.Name == "" {
					return fmt.Errorf("vpc subnet selector requires id or name: selector %+v", selector)
				}
				entry, err := r.getCachedSubnetByName(ctx, vpcID, selector.Name)
				if err != nil {
					return fmt.Errorf("failed to get subnet by name in vpc %s: %w", vpcID, err)
				}
				if entry == nil {
					return fmt.Errorf("subnet with name %s not found in vpc %s", selector.Name, vpcID)
				}
				id = entry.ID
			}
			subnet, err := r.ExoscaleClient.GetSubnet(ctx, vpcID, id)
			if err != nil {
				return fmt.Errorf("failed to get subnet %s in vpc %s: %w", id, vpcID, err)
			}
			subnetIDs = append(subnetIDs, subnet.ID.String())
		}
	}

	nodeClass.Status.VpcSubnets = subnetIDs
	nodeClass.Status.VpcID = vpcID.String()
	return nil
}

// reconcileConfigurationHash hashes every bootstrap-affecting field into
// status.configurationHash, which the cloudprovider compares against each
// NodeClaim's annotation to detect drift. Only fields the user configured
// participate, so removing an override yields an empty hash that still
// differs from the stale one on existing NodeClaims.
func (r *ExoscaleNodeClassReconciler) reconcileConfigurationHash(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) error {
	var entries []hashEntry

	if spec := nodeClass.Spec.ContainerRegistry; spec != nil {
		registryEntries, err := r.containerRegistryHashEntries(ctx, spec)
		if err != nil {
			return err
		}
		entries = append(entries, registryEntries...)
	}

	kubelet := nodeClass.Spec.Kubelet
	if pol := kubelet.CPUManagerPolicy; pol != "" && pol != "none" {
		entries = append(entries, hashEntry{key: "cpumanager/policy", value: []byte(pol)})
	}
	if len(kubelet.CPUManagerPolicyOptions) > 0 && kubelet.CPUManagerPolicy == "static" {
		opts := slices.Sorted(slices.Values(kubelet.CPUManagerPolicyOptions))
		entries = append(entries, hashEntry{key: "cpumanager/options", value: []byte(strings.Join(opts, ","))})
	}
	if p := kubelet.CPUManagerReconcilePeriod; p != "" && p != apiv1.DefaultCPUManagerReconcilePeriod {
		entries = append(entries, hashEntry{key: "cpumanager/reconcilePeriod", value: []byte(p)})
	}
	if kubelet.MaxPods != nil {
		entries = append(entries, hashEntry{
			key:   "maxpods/value",
			value: fmt.Appendf([]byte{}, "%d", *kubelet.MaxPods),
		})
	}

	nodeClass.Status.ConfigurationHash = hashEntries(entries)
	return nil
}

// containerRegistryHashEntries goes through userdata.RegistryResolver so the
// reconciler and the instance provider validate Secrets identically.
func (r *ExoscaleNodeClassReconciler) containerRegistryHashEntries(ctx context.Context, spec *apiv1.ContainerRegistrySpec) ([]hashEntry, error) {
	resolved, err := userdata.NewRegistryResolver(r.Client).Resolve(ctx, spec)
	if err != nil {
		return nil, err
	}

	var entries []hashEntry

	for _, mirror := range resolved.Mirrors {
		for _, ep := range mirror.Endpoints {
			if ep.TLS == nil {
				continue
			}
			if len(ep.TLS.CA) > 0 {
				entries = append(entries, hashEntry{
					key:   fmt.Sprintf("registry/mirror/%s/endpoint/%s/tls/ca.crt", mirror.Registry, ep.URL),
					value: ep.TLS.CADec,
				})
			}
			if len(ep.TLS.Cert) > 0 {
				entries = append(entries, hashEntry{
					key:   fmt.Sprintf("registry/mirror/%s/endpoint/%s/tls/tls.crt", mirror.Registry, ep.URL),
					value: ep.TLS.CertDec,
				})
				entries = append(entries, hashEntry{
					key:   fmt.Sprintf("registry/mirror/%s/endpoint/%s/tls/tls.key", mirror.Registry, ep.URL),
					value: ep.TLS.KeyDec,
				})
			}
		}
	}

	for _, credential := range resolved.Credentials {
		// Hash the secret bytes so rotating a referenced Secret triggers drift.
		var value []byte
		switch credential.Kind {
		case "basic":
			value = append(append([]byte{}, credential.Username...), 0)
			value = append(value, credential.Password...)
		case "auth":
			value = credential.Auth
		case "identitytoken":
			value = credential.IdentityToken
		}
		entries = append(entries, hashEntry{
			key:   fmt.Sprintf("registry/credential/%s/%s", credential.Registry, credential.Kind),
			value: value,
		})
	}

	return entries, nil
}
