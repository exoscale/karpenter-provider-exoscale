package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	egov3 "github.com/exoscale/egoscale/v3"
	"github.com/exoscale/egoscale/v3/credentials"
	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-exoscale/pkg/constants"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

func main() {
	var runID string
	flag.StringVar(&runID, "run-id", "", "Test run ID to cleanup (required, e.g., e2e-20251224-143052-abc123)")
	flag.Parse()

	if runID == "" {
		fmt.Fprintln(os.Stderr, "Error: --run-id flag is required")
		fmt.Fprintln(os.Stderr, "Usage: go run ./test/e2e/cmd/cleanup/main.go --run-id=<run-id>")
		os.Exit(1)
	}

	if !strings.HasPrefix(runID, "e2e-") {
		fmt.Fprintln(os.Stderr, "Error: run-id must start with 'e2e-'")
		os.Exit(1)
	}

	ctx := context.Background()

	if err := cleanup(ctx, runID); err != nil {
		fmt.Fprintf(os.Stderr, "Cleanup failed: %v\n", err)
		os.Exit(1)
	}
}

func cleanup(ctx context.Context, runID string) error {
	fmt.Printf("Cleaning up resources for run ID: %s\n", runID)

	kubeClient, err := buildKubeClient()
	if err != nil {
		return fmt.Errorf("failed to create kube client: %w", err)
	}

	if err := cleanupNodeClaims(ctx, kubeClient, runID); err != nil {
		fmt.Printf("Warning: failed to cleanup NodeClaims: %v\n", err)
	}

	if err := cleanupNodePools(ctx, kubeClient, runID); err != nil {
		fmt.Printf("Warning: failed to cleanup NodePools: %v\n", err)
	}

	if err := cleanupNodeClasses(ctx, kubeClient, runID); err != nil {
		fmt.Printf("Warning: failed to cleanup NodeClasses: %v\n", err)
	}

	exoClient, err := buildExoscaleClient()
	if err != nil {
		return fmt.Errorf("failed to create exoscale client: %w", err)
	}

	if err := cleanupInstances(ctx, exoClient, runID); err != nil {
		fmt.Printf("Warning: failed to cleanup instances: %v\n", err)
	}

	if err := cleanupPrivateNetworks(ctx, exoClient, runID); err != nil {
		fmt.Printf("Warning: failed to cleanup private networks: %v\n", err)
	}

	if err := cleanupSKSClusters(ctx, exoClient, runID); err != nil {
		fmt.Printf("Warning: failed to cleanup SKS clusters: %v\n", err)
	}

	fmt.Println("Cleanup completed")
	return nil
}

func cleanupNodeClaims(ctx context.Context, kubeClient client.Client, runID string) error {
	var list karpenterv1.NodeClaimList
	if err := kubeClient.List(ctx, &list); err != nil {
		return err
	}

	for _, item := range list.Items {
		if strings.HasPrefix(item.Name, runID) {
			fmt.Printf("Deleting NodeClaim: %s\n", item.Name)
			if err := kubeClient.Delete(ctx, &item); err != nil {
				fmt.Printf("  Warning: failed to delete: %v\n", err)
			}
		}
	}
	return nil
}

func cleanupNodePools(ctx context.Context, kubeClient client.Client, runID string) error {
	var list karpenterv1.NodePoolList
	if err := kubeClient.List(ctx, &list); err != nil {
		return err
	}

	for _, item := range list.Items {
		if strings.HasPrefix(item.Name, runID) {
			fmt.Printf("Deleting NodePool: %s\n", item.Name)
			if err := kubeClient.Delete(ctx, &item); err != nil {
				fmt.Printf("  Warning: failed to delete: %v\n", err)
			}
		}
	}
	return nil
}

func cleanupNodeClasses(ctx context.Context, kubeClient client.Client, runID string) error {
	var list apiv1.ExoscaleNodeClassList
	if err := kubeClient.List(ctx, &list); err != nil {
		return err
	}

	for _, item := range list.Items {
		if strings.HasPrefix(item.Name, runID) {
			fmt.Printf("Deleting NodeClass: %s\n", item.Name)
			if err := kubeClient.Delete(ctx, &item); err != nil {
				fmt.Printf("  Warning: failed to delete: %v\n", err)
			}
		}
	}
	return nil
}

func cleanupInstances(ctx context.Context, exoClient *egov3.Client, runID string) error {
	instances, err := exoClient.ListInstances(ctx)
	if err != nil {
		return err
	}

	for _, inst := range instances.Instances {
		if !strings.HasPrefix(string(inst.Name), runID) {
			continue
		}

		if inst.Labels == nil {
			continue
		}

		if inst.Labels[constants.InstanceLabelManagedBy] != constants.ManagedByKarpenter {
			continue
		}

		fmt.Printf("Deleting instance: %s (%s)\n", inst.Name, inst.ID)
		op, err := exoClient.DeleteInstance(ctx, inst.ID)
		if err != nil {
			fmt.Printf("  Warning: failed to delete: %v\n", err)
			continue
		}
		if _, err := exoClient.Wait(ctx, op, egov3.OperationStateSuccess); err != nil {
			fmt.Printf("  Warning: failed to wait for deletion: %v\n", err)
		}
	}
	return nil
}

func cleanupPrivateNetworks(ctx context.Context, exoClient *egov3.Client, runID string) error {
	networks, err := exoClient.ListPrivateNetworks(ctx)
	if err != nil {
		return err
	}

	for _, pn := range networks.PrivateNetworks {
		if !strings.HasPrefix(string(pn.Name), runID) {
			continue
		}

		fmt.Printf("Deleting private network: %s (%s)\n", pn.Name, pn.ID)
		op, err := exoClient.DeletePrivateNetwork(ctx, pn.ID)
		if err != nil {
			fmt.Printf("  Warning: failed to delete: %v\n", err)
			continue
		}
		if _, err := exoClient.Wait(ctx, op, egov3.OperationStateSuccess); err != nil {
			fmt.Printf("  Warning: failed to wait for deletion: %v\n", err)
		}
	}
	return nil
}

func cleanupSKSClusters(ctx context.Context, exoClient *egov3.Client, runID string) error {
	clusters, err := exoClient.ListSKSClusters(ctx)
	if err != nil {
		return err
	}

	for _, cluster := range clusters.SKSClusters {
		if !strings.HasPrefix(string(cluster.Name), runID) {
			continue
		}

		for _, nodepool := range cluster.Nodepools {
			fmt.Printf("Deleting SKS nodepool: %s\n", nodepool.Name)
			op, err := exoClient.DeleteSKSNodepool(ctx, cluster.ID, nodepool.ID)
			if err != nil {
				fmt.Printf("  Warning: failed to delete nodepool: %v\n", err)
				continue
			}
			if _, err := exoClient.Wait(ctx, op, egov3.OperationStateSuccess); err != nil {
				fmt.Printf("  Warning: failed to wait for nodepool deletion: %v\n", err)
			}
		}

		fmt.Printf("Deleting SKS cluster: %s (%s)\n", cluster.Name, cluster.ID)
		op, err := exoClient.DeleteSKSCluster(ctx, cluster.ID)
		if err != nil {
			fmt.Printf("  Warning: failed to delete: %v\n", err)
			continue
		}
		if _, err := exoClient.Wait(ctx, op, egov3.OperationStateSuccess); err != nil {
			fmt.Printf("  Warning: failed to wait for deletion: %v\n", err)
		}
	}
	return nil
}

func buildKubeClient() (client.Client, error) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = apiv1.SchemeBuilder.AddToScheme(scheme)

	karpenterGV := schema.GroupVersion{Group: "karpenter.sh", Version: "v1"}
	scheme.AddKnownTypes(karpenterGV,
		&karpenterv1.NodePool{},
		&karpenterv1.NodePoolList{},
		&karpenterv1.NodeClaim{},
		&karpenterv1.NodeClaimList{})
	metav1.AddToGroupVersion(scheme, karpenterGV)

	cfg, err := config.GetConfig()
	if err != nil {
		return nil, err
	}

	return client.New(cfg, client.Options{Scheme: scheme})
}

func buildExoscaleClient() (*egov3.Client, error) {
	apiKey := os.Getenv("EXOSCALE_API_KEY")
	apiSecret := os.Getenv("EXOSCALE_API_SECRET")
	zone := os.Getenv("EXOSCALE_ZONE")
	apiEndpoint := os.Getenv("EXOSCALE_API_ENDPOINT")

	if apiKey == "" || apiSecret == "" {
		return nil, fmt.Errorf("EXOSCALE_API_KEY and EXOSCALE_API_SECRET are required")
	}

	var opts []egov3.ClientOpt
	if apiEndpoint != "" {
		opts = append(opts, egov3.ClientOptWithEndpoint(egov3.Endpoint(apiEndpoint)))
	}

	exoClient, err := egov3.NewClient(
		credentials.NewStaticCredentials(apiKey, apiSecret),
		opts...,
	)
	if err != nil {
		return nil, err
	}

	if apiEndpoint == "" && zone != "" {
		endpoint, err := exoClient.GetZoneAPIEndpoint(context.Background(), egov3.ZoneName(zone))
		if err != nil {
			return nil, err
		}
		return egov3.NewClient(
			credentials.NewStaticCredentials(apiKey, apiSecret),
			egov3.ClientOptWithEndpoint(endpoint),
		)
	}

	return exoClient, nil
}
