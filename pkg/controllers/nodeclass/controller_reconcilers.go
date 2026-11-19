package nodeclass

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	egov3 "github.com/exoscale/egoscale/v3"
	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/providers/userdata"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// hashEntry is a key/value pair fed into the SHA256 hash used for drift
// detection. Kept unexported so the hashing convention stays an internal
// detail of this package.
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
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues("nodeclass", nodeClass.Name))

	t, err := r.TemplateResolver.ResolveTemplate(ctx, nodeClass)
	if err != nil {
		return fmt.Errorf("failed to resolve template ID: %w", err)
	}

	log.FromContext(ctx).V(1).Info("verifying template", "templateID", t.ID)
	if _, err := r.ExoscaleClient.GetTemplate(ctx, egov3.UUID(t.ID)); err != nil {
		return fmt.Errorf("template %s not found or not accessible: %w", t.ID, err)
	}

	nodeClass.Status.ImageID = t.ID
	return nil
}

func (r *ExoscaleNodeClassReconciler) reconcileSecurityGroups(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) error {
	sgIDs := []string{}

	// Deprecated field, use selector instead now
	for _, sgID := range nodeClass.Spec.SecurityGroups {
		log.FromContext(ctx).V(1).Info("resolving security group", "securityGroupID", sgID)
		sg, err := r.ExoscaleClient.GetSecurityGroup(ctx, egov3.UUID(sgID))
		if err != nil {
			log.FromContext(ctx).Error(err, "failed to get security group", "securityGroupID", sgID)
			return fmt.Errorf("failed to get security group %s: %w", sgID, err)
		}
		sgIDs = append(sgIDs, sg.ID.String())
	}

	for _, selector := range nodeClass.Spec.SecurityGroupSelectorTerms {
		var sg *egov3.SecurityGroup
		var err error
		if selector.ID != "" {
			log.FromContext(ctx).V(1).Info("resolving security group by ID", "securityGroupID", selector.ID)
			sg, err = r.ExoscaleClient.GetSecurityGroup(ctx, egov3.UUID(selector.ID))
		} else if selector.Name != "" {
			log.FromContext(ctx).V(1).Info("resolving security group by Name", "securityGroupName", selector.Name)
			// Discover security group by name
			sg, err = r.getCachedSecurityGroupByName(ctx, selector.Name)
			if err != nil {
				log.FromContext(ctx).Error(err, "failed to get security group by name")
				return fmt.Errorf("failed to get security group by name: %w", err)
			}

			if sg == nil {
				err = fmt.Errorf("security group with name %s not found", selector.Name)
			}
		}
		if err != nil {
			log.FromContext(ctx).Error(err, "failed to get security group", "selector", selector)
			return fmt.Errorf("failed to get security group for selector %+v: %w", selector, err)
		}

		sgIDs = append(sgIDs, sg.ID.String())
	}

	nodeClass.Status.SecurityGroups = sgIDs
	return nil
}

func (r *ExoscaleNodeClassReconciler) reconcileAntiAffinityGroups(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) error {
	aagIDs := []string{}

	// Deprecated field, use selector instead now
	for _, aagID := range nodeClass.Spec.AntiAffinityGroups {
		log.FromContext(ctx).V(1).Info("resolving anti-affinity group", "antiAffinityGroupID", aagID)
		aag, err := r.ExoscaleClient.GetAntiAffinityGroup(ctx, egov3.UUID(aagID))
		if err != nil {
			log.FromContext(ctx).Error(err, "failed to get anti-affinity group", "antiAffinityGroupID", aagID)
			return fmt.Errorf("failed to get anti-affinity group %s: %w", aagID, err)
		}
		aagIDs = append(aagIDs, aag.ID.String())
	}

	for _, selector := range nodeClass.Spec.AntiAffinityGroupSelectorTerms {
		var aag *egov3.AntiAffinityGroup
		var err error
		if selector.ID != "" {
			log.FromContext(ctx).V(1).Info("resolving anti-affinity group by ID", "antiAffinityGroupID", selector.ID)
			aag, err = r.ExoscaleClient.GetAntiAffinityGroup(ctx, egov3.UUID(selector.ID))
		} else if selector.Name != "" {
			log.FromContext(ctx).V(1).Info("resolving anti-affinity group by Name", "antiAffinityGroupName", selector.Name)
			// Discover anti-affinity group by name
			aag, err = r.getCachedAntiAffinityGroupByName(ctx, selector.Name)
			if err != nil {
				log.FromContext(ctx).Error(err, "failed to get anti-affinity group by name")
				return fmt.Errorf("failed to get anti-affinity group by name: %w", err)
			}

			if aag == nil {
				err = fmt.Errorf("anti-affinity group with name %s not found", selector.Name)
			}
		}
		if err != nil {
			log.FromContext(ctx).Error(err, "failed to get anti-affinity group", "selector", selector)
			return fmt.Errorf("failed to get anti-affinity group for selector %+v: %w", selector, err)
		}

		aagIDs = append(aagIDs, aag.ID.String())
	}

	nodeClass.Status.AntiAffinityGroups = aagIDs
	return nil
}

func (r *ExoscaleNodeClassReconciler) reconcileElasticIPs(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) error {
	ids := []string{}
	for _, selector := range nodeClass.Spec.ElasticIPSelectorTerms {
		var eip *egov3.ElasticIP
		var err error
		switch {
		case selector.ID != "":
			log.FromContext(ctx).V(1).Info("resolving elastic IP by ID", "elasticIPID", selector.ID)
			eip, err = r.ExoscaleClient.GetElasticIP(ctx, egov3.UUID(selector.ID))
		case selector.Name != "":
			// Elastic IPs don't have a "name" attribute in the Exoscale API;
			// we treat the selector `name` field as the IP address to match
			// against `ElasticIP.IP`.
			log.FromContext(ctx).V(1).Info("resolving elastic IP by address", "elasticIPAddress", selector.Name)
			eip, err = r.getCachedElasticIPByAddress(ctx, selector.Name)
			if err == nil && eip == nil {
				err = fmt.Errorf("elastic IP with address %s not found", selector.Name)
			}
		default:
			err = fmt.Errorf("elastic IP selector requires id or name: selector %+v", selector)
		}
		if err != nil {
			return fmt.Errorf("failed to get elastic IP for selector %+v: %w", selector, err)
		}
		ids = append(ids, eip.ID.String())
	}
	nodeClass.Status.ElasticIPs = ids
	return nil
}
func (r *ExoscaleNodeClassReconciler) reconcilePrivateNetworks(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) error {
	if len(nodeClass.Spec.PrivateNetworks) > 0 || len(nodeClass.Spec.PrivateNetworkSelectorTerms) > 0 {
		log.FromContext(ctx).Info("privateNetworks/privateNetworkSelectorTerms are deprecated, use vpcSubnetSelectorTerms instead")
	}

	privNetIDs := []string{}

	// Deprecated field, use selector instead now
	for _, netID := range nodeClass.Spec.PrivateNetworks {
		log.FromContext(ctx).V(1).Info("resolving private network", "privateNetworkID", netID)
		net, err := r.ExoscaleClient.GetPrivateNetwork(ctx, egov3.UUID(netID))
		if err != nil {
			log.FromContext(ctx).Error(err, "failed to get private network", "privateNetworkID", netID)
			return fmt.Errorf("failed to get private network %s: %w", netID, err)
		}
		privNetIDs = append(privNetIDs, net.ID.String())
	}

	for _, selector := range nodeClass.Spec.PrivateNetworkSelectorTerms {
		var net *egov3.PrivateNetwork
		var err error
		if selector.ID != "" {
			log.FromContext(ctx).V(1).Info("resolving private network by ID", "privateNetworkID", selector.ID)
			net, err = r.ExoscaleClient.GetPrivateNetwork(ctx, egov3.UUID(selector.ID))
		} else if selector.Name != "" {
			log.FromContext(ctx).V(1).Info("resolving private network by Name", "privateNetworkName", selector.Name)
			// Discover private network by name
			net, err = r.getCachedPrivateNetworkByName(ctx, selector.Name)
			if err != nil {
				log.FromContext(ctx).Error(err, "failed to get private network by name")
				return fmt.Errorf("failed to get private network by name: %w", err)
			}

			if net == nil {
				err = fmt.Errorf("private network with name %s not found", selector.Name)
			}
		}
		if err != nil {
			log.FromContext(ctx).Error(err, "failed to get private network", "selector", selector)
			return fmt.Errorf("failed to get private network for selector %+v: %w", selector, err)
		}

		privNetIDs = append(privNetIDs, net.ID.String())
	}

	nodeClass.Status.PrivateNetworks = privNetIDs
	return nil
}

// resolveVpc resolves spec.vpcSelector (by ID or name) into a VPC ID,
// verifying the VPC is accessible through the Get endpoint.
func (r *ExoscaleNodeClassReconciler) resolveVpc(ctx context.Context, selector *apiv1.SelectorTerms) (egov3.UUID, error) {
	if selector == nil {
		return "", fmt.Errorf("vpcSelector is required when vpcSubnetSelectorTerms is set")
	}

	var vpcID egov3.UUID
	if selector.ID != "" {
		log.FromContext(ctx).V(1).Info("resolving vpc by ID", "vpcID", selector.ID)
		vpc, err := r.ExoscaleClient.GetVpc(ctx, egov3.UUID(selector.ID))
		if err != nil {
			log.FromContext(ctx).Error(err, "failed to get vpc", "vpcID", selector.ID)
			return "", fmt.Errorf("failed to get vpc %s: %w", selector.ID, err)
		}
		vpcID = vpc.ID
	} else if selector.Name != "" {
		log.FromContext(ctx).V(1).Info("resolving vpc by Name", "vpcName", selector.Name)
		entry, err := r.getCachedVpcByName(ctx, selector.Name)
		if err != nil {
			log.FromContext(ctx).Error(err, "failed to get vpc by name")
			return "", fmt.Errorf("failed to get vpc by name: %w", err)
		}
		if entry == nil {
			return "", fmt.Errorf("vpc with name %s not found", selector.Name)
		}
		// Verify the VPC is accessible through the Get endpoint.
		vpc, err := r.ExoscaleClient.GetVpc(ctx, entry.ID)
		if err != nil {
			return "", fmt.Errorf("failed to get vpc %s (name %s): %w", entry.ID, selector.Name, err)
		}
		vpcID = vpc.ID
	} else {
		return "", fmt.Errorf("vpcSelector requires id or name: selector %+v", selector)
	}

	return vpcID, nil
}

// reconcileVpcSubnets resolves spec.vpcSelector into status.vpcID and
// spec.vpcSubnetSelectorTerms into status.vpcSubnets (subnet IDs within that
// VPC). An instance can only be attached to subnets of a single VPC, so the
// VPC is selected explicitly and subnets are only looked up inside it.
func (r *ExoscaleNodeClassReconciler) reconcileVpcSubnets(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) error {
	subnetIDs := []string{}
	vpcID := ""

	if len(nodeClass.Spec.VpcSubnetSelectorTerms) > 0 {
		resolvedVpcID, err := r.resolveVpc(ctx, nodeClass.Spec.VpcSelector)
		if err != nil {
			return err
		}
		vpcID = resolvedVpcID.String()

		for _, selector := range nodeClass.Spec.VpcSubnetSelectorTerms {
			var subnetID string
			if selector.ID != "" {
				log.FromContext(ctx).V(1).Info("resolving vpc subnet by ID", "subnetID", selector.ID, "vpcID", vpcID)
				subnet, err := r.ExoscaleClient.GetSubnet(ctx, resolvedVpcID, egov3.UUID(selector.ID))
				if err != nil {
					log.FromContext(ctx).Error(err, "failed to get vpc subnet", "subnetID", selector.ID, "vpcID", vpcID)
					return fmt.Errorf("failed to get subnet %s in vpc %s: %w", selector.ID, vpcID, err)
				}
				subnetID = subnet.ID.String()
			} else if selector.Name != "" {
				log.FromContext(ctx).V(1).Info("resolving vpc subnet by Name", "subnetName", selector.Name, "vpcID", vpcID)
				subnet, err := r.getCachedSubnetByName(ctx, resolvedVpcID, selector.Name)
				if err != nil {
					return fmt.Errorf("failed to get subnet by name in vpc %s: %w", vpcID, err)
				}
				if subnet == nil {
					return fmt.Errorf("subnet with name %s not found in vpc %s", selector.Name, vpcID)
				}
				// Verify the subnet is accessible through the Get endpoint.
				if _, err := r.ExoscaleClient.GetSubnet(ctx, resolvedVpcID, subnet.ID); err != nil {
					return fmt.Errorf("failed to get subnet %s in vpc %s: %w", subnet.ID, vpcID, err)
				}
				subnetID = subnet.ID.String()
			} else {
				return fmt.Errorf("vpc subnet selector requires id or name: selector %+v", selector)
			}
			subnetIDs = append(subnetIDs, subnetID)
		}
	}

	nodeClass.Status.VpcSubnets = subnetIDs
	nodeClass.Status.VpcID = vpcID
	return nil
}

// reconcileConfigurationHash computes a single SHA256 hash over every
// bootstrap-affecting field of the NodeClass (container registry Secret
// contents, kubelet CPU manager, kubelet maxPods) and stores it in
// nodeClass.Status.ConfigurationHash. The cloudprovider compares this value
// against the karpenter.exoscale.com/configuration-hash annotation on each
// NodeClaim to detect drift.
//
// Only fields the user has actually configured participate in the hash. When
// nothing is configured, the resulting hash is empty, which is the trigger
// for drift when the user later removes their override (the NodeClaim still
// carries a non-empty stale hash).
func (r *ExoscaleNodeClassReconciler) reconcileConfigurationHash(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) error {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues("nodeclass", nodeClass.Name))

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
		opts := append([]string(nil), kubelet.CPUManagerPolicyOptions...)
		sort.Strings(opts)
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

// containerRegistryHashEntries validates every Secret referenced from
// spec.containerRegistry (always resolved in the kube-system namespace) and
// returns the hash entries that contribute to the bootstrap configuration
// hash. Secret loading is delegated to the shared userdata.RegistryResolver
// so the provider and the reconciler apply identical validation rules.
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
		// Hash the resolved secret bytes, not just the Kind label. Without
		// this, rotating a referenced Secret in the cluster would not
		// change the configuration hash and would silently miss drift on
		// the running NodeClaim.
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
