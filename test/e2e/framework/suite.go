package framework

import (
	"context"
	"fmt"
	"sync"

	egov3 "github.com/exoscale/egoscale/v3"
	"github.com/exoscale/egoscale/v3/credentials"
	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

var Suite *E2ESuite

type E2ESuite struct {
	Config     *Config
	KubeClient client.Client
	ExoClient  *egov3.Client

	SKSClusterID egov3.UUID

	mu                     sync.Mutex
	createdInstances       []string
	createdNodeClasses     []types.UID
	createdNodePools       []types.UID
	createdNodeClaims      []types.UID
	createdPrivateNetworks []string
	createdSKSClusters     []string
}

func SetupSuite() {
	var err error
	Suite, err = NewE2ESuite()
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "Failed to create E2E suite")

	ginkgo.GinkgoWriter.Printf("Test run ID: %s\n", Suite.Config.TestRunID)
	ginkgo.GinkgoWriter.Printf("Zone: %s\n", Suite.Config.Zone)

	ctx := context.Background()

	if Suite.Config.SkipClusterCreate {
		ginkgo.GinkgoWriter.Printf("Using existing cluster: %s\n", Suite.Config.ClusterName)
		clusters, err := Suite.ExoClient.ListSKSClusters(ctx)
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "Failed to list SKS clusters")

		var found bool
		for _, c := range clusters.SKSClusters {
			if c.Name == Suite.Config.ClusterName {
				Suite.SKSClusterID = c.ID
				found = true
				break
			}
		}
		gomega.Expect(found).To(gomega.BeTrue(), "Cluster %s not found", Suite.Config.ClusterName)
	} else {
		cluster, err := Suite.CreateSKSCluster(ctx)
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "Failed to create SKS cluster")

		err = Suite.WaitForSKSClusterReady(ctx, cluster.ID, Suite.Config.ClusterCreateTimeout)
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "SKS cluster not ready")

		Suite.SKSClusterID = cluster.ID
	}

	restConfig, err := Suite.GetSKSClusterKubeconfig(ctx, Suite.SKSClusterID)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "Failed to get kubeconfig")

	Suite.KubeClient, err = client.New(restConfig, client.Options{Scheme: buildScheme()})
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "Failed to create kube client")

	if !Suite.Config.SkipKarpenterDeploy {
		err = Suite.DeployKarpenter(ctx)
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "Failed to deploy Karpenter")
	} else {
		ginkgo.GinkgoWriter.Println("Skipping Karpenter deployment (E2E_SKIP_KARPENTER_DEPLOY=true)")
	}

	err = Suite.VerifyPrerequisites(ctx)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "Prerequisites check failed")
}

func TeardownSuite() {
	if Suite == nil {
		return
	}

	ctx := context.Background()

	Suite.CleanupAllResources(ctx)

	if !Suite.Config.SkipClusterCreate && Suite.SKSClusterID != "" {
		if err := Suite.DeleteSKSCluster(ctx, Suite.SKSClusterID); err != nil {
			ginkgo.GinkgoWriter.Printf("Warning: failed to delete SKS cluster: %v\n", err)
		}
	}
}

func NewE2ESuite() (*E2ESuite, error) {
	cfg, err := LoadConfigFromEnv()
	if err != nil {
		return nil, err
	}

	exoClient, err := buildExoscaleClient(cfg)
	if err != nil {
		return nil, err
	}

	return &E2ESuite{
		Config:    cfg,
		ExoClient: exoClient,
	}, nil
}

func (s *E2ESuite) VerifyPrerequisites(ctx context.Context) error {
	ginkgo.GinkgoWriter.Println("Verifying prerequisites...")

	template, err := s.ExoClient.GetTemplate(ctx, egov3.UUID(s.Config.TemplateID))
	if err != nil {
		return fmt.Errorf("failed to get template %s: %w", s.Config.TemplateID, err)
	}
	ginkgo.GinkgoWriter.Printf("Template verified: %s\n", template.Name)

	sg, err := s.ExoClient.GetSecurityGroup(ctx, egov3.UUID(s.Config.SecurityGroupID))
	if err != nil {
		return fmt.Errorf("failed to get security group %s: %w", s.Config.SecurityGroupID, err)
	}
	ginkgo.GinkgoWriter.Printf("Security group verified: %s\n", sg.Name)

	var nodeClassList apiv1.ExoscaleNodeClassList
	if err := s.KubeClient.List(ctx, &nodeClassList); err != nil {
		return fmt.Errorf("failed to list NodeClasses (CRD may not be installed): %w", err)
	}
	ginkgo.GinkgoWriter.Printf("ExoscaleNodeClass CRD verified (found %d existing)\n", len(nodeClassList.Items))

	return nil
}

func (s *E2ESuite) TrackInstance(instanceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createdInstances = append(s.createdInstances, instanceID)
}

func (s *E2ESuite) TrackNodeClass(uid types.UID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createdNodeClasses = append(s.createdNodeClasses, uid)
}

func (s *E2ESuite) TrackNodePool(uid types.UID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createdNodePools = append(s.createdNodePools, uid)
}

func (s *E2ESuite) TrackNodeClaim(uid types.UID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createdNodeClaims = append(s.createdNodeClaims, uid)
}

func (s *E2ESuite) TrackPrivateNetwork(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createdPrivateNetworks = append(s.createdPrivateNetworks, id)
}

func (s *E2ESuite) CreateNodeClass(ctx context.Context, nc *apiv1.ExoscaleNodeClass) error {
	if err := s.KubeClient.Create(ctx, nc); err != nil {
		return err
	}
	s.TrackNodeClass(nc.UID)
	return nil
}

func (s *E2ESuite) CreateNodePool(ctx context.Context, np *karpenterv1.NodePool) error {
	if err := s.KubeClient.Create(ctx, np); err != nil {
		return err
	}
	s.TrackNodePool(np.UID)
	return nil
}

func (s *E2ESuite) CreateNodeClaim(ctx context.Context, nc *karpenterv1.NodeClaim) error {
	if err := s.KubeClient.Create(ctx, nc); err != nil {
		return err
	}
	s.TrackNodeClaim(nc.UID)
	return nil
}

func (s *E2ESuite) TrackSKSCluster(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createdSKSClusters = append(s.createdSKSClusters, id)
}

func (s *E2ESuite) ResourceName(suffix string) string {
	return s.Config.TestRunID + "-" + suffix
}

func buildScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = apiv1.SchemeBuilder.AddToScheme(scheme)
	_ = apiextensionsv1.AddToScheme(scheme)

	karpenterGV := schema.GroupVersion{Group: "karpenter.sh", Version: "v1"}
	scheme.AddKnownTypes(karpenterGV,
		&karpenterv1.NodePool{},
		&karpenterv1.NodePoolList{},
		&karpenterv1.NodeClaim{},
		&karpenterv1.NodeClaimList{})
	metav1.AddToGroupVersion(scheme, karpenterGV)

	return scheme
}

func buildExoscaleClient(cfg *Config) (*egov3.Client, error) {
	var opts []egov3.ClientOpt

	if cfg.APIEndpoint != "" {
		opts = append(opts, egov3.ClientOptWithEndpoint(egov3.Endpoint(cfg.APIEndpoint)))
	}

	exoClient, err := egov3.NewClient(
		credentials.NewStaticCredentials(cfg.APIKey, cfg.APISecret),
		opts...,
	)
	if err != nil {
		return nil, err
	}

	if cfg.APIEndpoint == "" {
		endpoint, err := exoClient.GetZoneAPIEndpoint(context.Background(), egov3.ZoneName(cfg.Zone))
		if err != nil {
			return nil, err
		}
		return egov3.NewClient(
			credentials.NewStaticCredentials(cfg.APIKey, cfg.APISecret),
			egov3.ClientOptWithEndpoint(endpoint),
		)
	}

	return exoClient, nil
}
