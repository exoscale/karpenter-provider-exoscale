// Package infra creates and destroys the disposable SKS clusters the e2e suite runs against.
package infra

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
	"github.com/exoscale/egoscale/v3/credentials"

	"github.com/exoscale/karpenter-provider-exoscale/pkg/constants"
)

// NamePrefix marks every resource the suite creates, so Stale never touches anything else.
const NamePrefix = "karpenter-e2e-"

type Cluster struct {
	ID egov3.UUID

	AdminKubeconfig     []byte
	KarpenterKubeconfig []byte

	// API key bound to the least-privilege IAM role from doc/iam.md.
	APIKey    string
	APISecret string
}

func Zone() string {
	if z := os.Getenv("EXOSCALE_ZONE"); z != "" {
		return z
	}
	return "ch-gva-2"
}

// NewClient builds a client from EXOSCALE_API_KEY/EXOSCALE_API_SECRET, honouring EXOSCALE_API_ENDPOINT.
func NewClient(ctx context.Context) (*egov3.Client, error) {
	key, secret := os.Getenv("EXOSCALE_API_KEY"), os.Getenv("EXOSCALE_API_SECRET")
	if key == "" || secret == "" {
		return nil, errors.New("EXOSCALE_API_KEY and EXOSCALE_API_SECRET are required")
	}
	creds := credentials.NewStaticCredentials(key, secret)

	endpoint := egov3.Endpoint(os.Getenv("EXOSCALE_API_ENDPOINT"))
	if endpoint == "" {
		c, err := egov3.NewClient(creds)
		if err != nil {
			return nil, err
		}
		if endpoint, err = c.GetZoneAPIEndpoint(ctx, egov3.ZoneName(Zone())); err != nil {
			return nil, fmt.Errorf("resolve endpoint for zone %s: %w", Zone(), err)
		}
	}
	return egov3.NewClient(creds, egov3.ClientOptWithEndpoint(endpoint))
}

func Versions(ctx context.Context, c *egov3.Client) ([]string, error) {
	resp, err := c.ListSKSClusterVersions(ctx)
	if err != nil {
		return nil, err
	}
	return resp.SKSClusterVersions, nil
}

// Create provisions an SKS cluster without the karpenter addon, a one-node
// nodepool for system pods, kubeconfigs, and a least-privilege API key.
// On error, whatever was created is left for Destroy(name).
func Create(ctx context.Context, c *egov3.Client, name, version string, policy egov3.IAMPolicy) (*Cluster, error) {
	systemType, err := InstanceType(ctx, c, "standard", "medium")
	if err != nil {
		return nil, err
	}

	op, err := c.CreateSKSCluster(ctx, egov3.CreateSKSClusterRequest{
		Name:                       name,
		Version:                    version,
		Level:                      egov3.CreateSKSClusterRequestLevelStarter,
		Cni:                        egov3.CreateSKSClusterRequestCniCalico,
		CreateDefaultSecurityGroup: new(true),
		Addons:                     []string{"exoscale-cloud-controller"},
	})
	if err != nil {
		return nil, fmt.Errorf("create SKS cluster: %w", err)
	}
	if op, err = c.Wait(ctx, op, egov3.OperationStateSuccess); err != nil {
		return nil, fmt.Errorf("wait for SKS cluster: %w", err)
	}
	sks, err := c.GetSKSCluster(ctx, op.Reference.ID)
	if err != nil {
		return nil, err
	}
	if sks.DefaultSecurityGroupID == nil {
		return nil, errors.New("SKS cluster has no default security group")
	}

	op, err = c.CreateSKSNodepool(ctx, sks.ID, egov3.CreateSKSNodepoolRequest{
		Name:           "system",
		Size:           1,
		DiskSize:       50,
		InstanceType:   &egov3.InstanceType{ID: systemType},
		SecurityGroups: []egov3.SecurityGroup{{ID: *sks.DefaultSecurityGroupID}},
	})
	if err != nil {
		return nil, fmt.Errorf("create system nodepool: %w", err)
	}
	if _, err = c.Wait(ctx, op, egov3.OperationStateSuccess); err != nil {
		return nil, fmt.Errorf("wait for system nodepool: %w", err)
	}

	cluster := &Cluster{ID: sks.ID}
	if cluster.AdminKubeconfig, err = kubeconfig(ctx, c, sks.ID, "kubernetes-admin", "system:masters"); err != nil {
		return nil, err
	}
	// Same identity as the SKS-managed Karpenter: CN karpenter-<cluster-id>, empty organization.
	if cluster.KarpenterKubeconfig, err = kubeconfig(ctx, c, sks.ID, "karpenter-"+sks.ID.String(), ""); err != nil {
		return nil, err
	}

	op, err = c.CreateIAMRole(ctx, egov3.CreateIAMRoleRequest{Name: name, Policy: &policy})
	if err != nil {
		return nil, fmt.Errorf("create IAM role: %w", err)
	}
	if op, err = c.Wait(ctx, op, egov3.OperationStateSuccess); err != nil {
		return nil, fmt.Errorf("wait for IAM role: %w", err)
	}
	key, err := c.CreateAPIKey(ctx, egov3.CreateAPIKeyRequest{Name: name, RoleID: op.Reference.ID})
	if err != nil {
		return nil, fmt.Errorf("create API key: %w", err)
	}
	cluster.APIKey, cluster.APISecret = key.Key, key.Secret

	return cluster, nil
}

func InstanceType(ctx context.Context, c *egov3.Client, family, size string) (egov3.UUID, error) {
	types, err := c.ListInstanceTypes(ctx)
	if err != nil {
		return "", err
	}
	for _, t := range types.InstanceTypes {
		if string(t.Family) == family && string(t.Size) == size {
			return t.ID, nil
		}
	}
	return "", fmt.Errorf("instance type %s.%s not found", family, size)
}

func kubeconfig(ctx context.Context, c *egov3.Client, id egov3.UUID, user, group string) ([]byte, error) {
	resp, err := c.GenerateSKSClusterKubeconfig(ctx, id, egov3.SKSKubeconfigRequest{
		User:   user,
		Groups: []string{group},
		Ttl:    int64((4 * time.Hour).Seconds()),
	})
	if err != nil {
		return nil, fmt.Errorf("generate kubeconfig for %s: %w", user, err)
	}
	return base64.StdEncoding.DecodeString(resp.Kubeconfig)
}

// Destroy deletes everything named name, discovering it from the API so it
// works after a crash or a cancelled job. Karpenter instances are not part of
// the SKS nodepool, so they are found by their cluster-id label.
func Destroy(ctx context.Context, c *egov3.Client, name string) error {
	var errs []error

	clusters, err := c.ListSKSClusters(ctx)
	if err != nil {
		return err
	}
	for _, sks := range clusters.SKSClusters {
		if sks.Name != name {
			continue
		}

		instances, err := c.ListInstances(ctx)
		if err != nil {
			errs = append(errs, err)
		} else {
			for _, i := range instances.Instances {
				if i.Labels[constants.InstanceLabelClusterID] == sks.ID.String() {
					errs = append(errs, wait(ctx, c)(c.DeleteInstance(ctx, i.ID)))
				}
			}
		}
		for _, np := range sks.Nodepools {
			errs = append(errs, wait(ctx, c)(c.DeleteSKSNodepool(ctx, sks.ID, np.ID)))
		}
		errs = append(errs, wait(ctx, c)(c.DeleteSKSCluster(ctx, sks.ID)))
		if sks.DefaultSecurityGroupID != nil {
			errs = append(errs, wait(ctx, c)(c.DeleteSecurityGroup(ctx, *sks.DefaultSecurityGroupID)))
		}
	}

	// Scenarios name the resources they create name or name-<suffix> (elastic
	// IPs have no name, so their description is used). Instances are gone by
	// now, so nothing is attached to them any more.
	owned := func(n string) bool { return n == name || strings.HasPrefix(n, name+"-") }

	if networks, err := c.ListPrivateNetworks(ctx); err != nil {
		errs = append(errs, err)
	} else {
		for _, pn := range networks.PrivateNetworks {
			if owned(pn.Name) {
				errs = append(errs, wait(ctx, c)(c.DeletePrivateNetwork(ctx, pn.ID)))
			}
		}
	}
	if eips, err := c.ListElasticIPS(ctx); err != nil {
		errs = append(errs, err)
	} else {
		for _, eip := range eips.ElasticIPS {
			if owned(eip.Description) {
				errs = append(errs, wait(ctx, c)(c.DeleteElasticIP(ctx, eip.ID)))
			}
		}
	}
	if groups, err := c.ListSecurityGroups(ctx); err != nil {
		errs = append(errs, err)
	} else {
		for _, sg := range groups.SecurityGroups {
			if owned(sg.Name) {
				errs = append(errs, wait(ctx, c)(c.DeleteSecurityGroup(ctx, sg.ID)))
			}
		}
	}
	if groups, err := c.ListAntiAffinityGroups(ctx); err != nil {
		errs = append(errs, err)
	} else {
		for _, aag := range groups.AntiAffinityGroups {
			if owned(aag.Name) {
				errs = append(errs, wait(ctx, c)(c.DeleteAntiAffinityGroup(ctx, aag.ID)))
			}
		}
	}
	if vpcs, err := c.ListVpcs(ctx); err != nil {
		errs = append(errs, err)
	} else {
		for _, vpc := range vpcs.Vpcs {
			if !owned(vpc.Name) {
				continue
			}
			subnets, err := c.ListSubnets(ctx, vpc.ID)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			for _, subnet := range subnets.Subnets {
				errs = append(errs, ignoreNotFound(c.DeleteSubnet(ctx, vpc.ID, subnet.ID)))
			}
			errs = append(errs, ignoreNotFound(c.DeleteVpc(ctx, vpc.ID)))
		}
	}

	keys, err := c.ListAPIKeys(ctx)
	if err != nil {
		errs = append(errs, err)
	} else {
		for _, k := range keys.APIKeys {
			if k.Name == name {
				errs = append(errs, wait(ctx, c)(c.DeleteAPIKey(ctx, k.Key)))
			}
		}
	}
	roles, err := c.ListIAMRoles(ctx)
	if err != nil {
		errs = append(errs, err)
	} else {
		for _, r := range roles.IAMRoles {
			if r.Name == name {
				errs = append(errs, wait(ctx, c)(c.DeleteIAMRole(ctx, r.ID)))
			}
		}
	}

	return errors.Join(errs...)
}

// Stale returns the names of clusters, and of IAM roles left without a
// cluster, that carry NamePrefix and are older than maxAge.
func Stale(ctx context.Context, c *egov3.Client, maxAge time.Duration) ([]string, error) {
	clusters, err := c.ListSKSClusters(ctx)
	if err != nil {
		return nil, err
	}
	live := map[string]bool{}
	var names []string
	for _, sks := range clusters.SKSClusters {
		if !strings.HasPrefix(sks.Name, NamePrefix) {
			continue
		}
		live[sks.Name] = true
		if time.Since(sks.CreatedAT) > maxAge {
			names = append(names, sks.Name)
		}
	}

	// The role is created after the cluster, so a prefixed role without a
	// cluster can only be left over from a teardown that did not finish.
	roles, err := c.ListIAMRoles(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range roles.IAMRoles {
		if strings.HasPrefix(r.Name, NamePrefix) && !live[r.Name] {
			names = append(names, r.Name)
		}
	}
	return names, nil
}

func ignoreNotFound(err error) error {
	if errors.Is(err, egov3.ErrNotFound) {
		return nil
	}
	return err
}

func wait(ctx context.Context, c *egov3.Client) func(*egov3.Operation, error) error {
	return func(op *egov3.Operation, err error) error {
		if err != nil {
			return ignoreNotFound(err)
		}
		_, err = c.Wait(ctx, op, egov3.OperationStateSuccess)
		return err
	}
}
