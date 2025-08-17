package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
	"github.com/exoscale/egoscale/v3/credentials"
	exoscale "github.com/exoscale/karpenter-exoscale/pkg/cloudprovider"
	"github.com/exoscale/karpenter-exoscale/pkg/controllers/bootstraptoken"
	"github.com/exoscale/karpenter-exoscale/pkg/controllers/garbagecollection"
	"github.com/exoscale/karpenter-exoscale/pkg/controllers/nodeclaim"
	"github.com/exoscale/karpenter-exoscale/pkg/controllers/nodeclass"
	"github.com/exoscale/karpenter-exoscale/pkg/controllers/repair"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/instance"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/instancetype"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/pricing"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/userdata"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/karpenter/pkg/controllers"
	"sigs.k8s.io/karpenter/pkg/controllers/state"
	"sigs.k8s.io/karpenter/pkg/events"
	"sigs.k8s.io/karpenter/pkg/operator"
)

func main() {
	ctx := setupSignalHandler()
	ctxOp, op := operator.NewOperator()

	if err := run(ctx, ctxOp, op); err != nil {
		op.GetLogger().Error(err, "operator failed")
		os.Exit(1)
	}
}

func run(ctx context.Context, ctxOp context.Context, op *operator.Operator) error {
	config, err := loadConfiguration()
	if err != nil {
		return err
	}

	exoClient, err := createExoscaleClient(ctx, config.Zone, config.APIKey, config.APISecret)
	if err != nil {
		return fmt.Errorf("failed to create Exoscale client: %w", err)
	}

	if err := validateZone(ctx, exoClient, config.Zone); err != nil {
		return fmt.Errorf("zone validation failed: %w", err)
	}

	pricingProvider, err := createPricingProvider()
	if err != nil {
		return fmt.Errorf("failed to create pricing provider: %w", err)
	}

	instanceTypeProvider, err := createInstanceTypeProvider(ctx, exoClient, config.Zone, pricingProvider)
	if err != nil {
		return fmt.Errorf("failed to create instance type provider: %w", err)
	}

	instanceProvider := createInstanceProvider(exoClient, config.Zone, config.ClusterName)

	userDataProvider := createUserDataProvider(op.GetClient(), op.GetConfig().Host, config.ClusterDNS, config.ClusterDomain)

	cloudProvider := exoscale.NewCloudProvider(
		op.GetClient(),
		exoClient,
		op.GetConfig().Host,
		op.EventRecorder,
		instanceTypeProvider,
		pricingProvider,
		instanceProvider,
		userDataProvider,
		config.Zone,
		config.ClusterName,
		config.ClusterDNS,
		config.ClusterDomain,
	)

	clusterState := state.NewCluster(op.Clock, op.GetClient(), cloudProvider)

	controllerList := controllers.NewControllers(
		ctxOp,
		op.Manager,
		op.Clock,
		op.GetClient(),
		op.EventRecorder,
		cloudProvider,
		clusterState,
	)

	if err := registerCustomControllers(op.Manager, exoClient, cloudProvider, instanceProvider, op.EventRecorder, config.Zone, config.ClusterName); err != nil {
		return fmt.Errorf("failed to register custom controllers: %w", err)
	}

	op.WithControllers(ctxOp, controllerList...).Start(ctxOp)

	return nil
}

type Config struct {
	Zone          string
	ClusterName   string
	APIKey        string
	APISecret     string
	ClusterDNS    string
	ClusterDomain string
}

func loadConfiguration() (*Config, error) {
	config := &Config{}

	var err error
	config.Zone, err = getRequiredEnv("EXOSCALE_ZONE")
	if err != nil {
		return nil, err
	}

	config.ClusterName, err = getRequiredEnv("CLUSTER_NAME")
	if err != nil {
		return nil, err
	}

	config.APIKey, err = getRequiredEnv("EXOSCALE_API_KEY")
	if err != nil {
		return nil, err
	}

	config.APISecret, err = getRequiredEnv("EXOSCALE_API_SECRET")
	if err != nil {
		return nil, err
	}

	config.ClusterDNS = os.Getenv("CLUSTER_DNS_IP")
	config.ClusterDomain = os.Getenv("CLUSTER_DOMAIN")

	return config, nil
}

func getRequiredEnv(key string) (string, error) {
	value := os.Getenv(key)
	if value == "" {
		return "", fmt.Errorf("%s environment variable is required", key)
	}
	return value, nil
}

func createExoscaleClient(ctx context.Context, zone, apiKey, apiSecret string) (*egov3.Client, error) {
	exoClient, err := egov3.NewClient(
		credentials.NewStaticCredentials(apiKey, apiSecret),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Exoscale client: %w", err)
	}

	endpoint, err := exoClient.GetZoneAPIEndpoint(ctx, egov3.ZoneName(zone))
	if err != nil {
		return nil, fmt.Errorf("failed to get zone endpoint for %s: %w", zone, err)
	}

	return egov3.NewClient(
		credentials.NewStaticCredentials(apiKey, apiSecret),
		egov3.ClientOptWithEndpoint(endpoint),
	)
}

func setupSignalHandler() context.Context {
	ctx, cancel := context.WithCancel(context.Background())

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		if _, err := fmt.Fprintf(os.Stderr, "Received signal: %v, initiating graceful shutdown...\n", sig); err != nil {
			// Best effort logging - if stderr write fails during shutdown, continue anyway
		}
		cancel()

		time.Sleep(10 * time.Second)
		if _, err := fmt.Fprintf(os.Stderr, "Graceful shutdown timeout exceeded, forcing exit\n"); err != nil {
			// Best effort logging - if stderr write fails during shutdown, continue anyway
		}
		os.Exit(1)
	}()

	return ctx
}

func validateZone(ctx context.Context, client *egov3.Client, zone string) error {
	zones, err := client.ListZones(ctx)
	if err != nil {
		return fmt.Errorf("failed to list zones: %w", err)
	}

	for _, z := range zones.Zones {
		if string(z.Name) == zone {
			return nil
		}
	}

	var availableZones []string
	for _, z := range zones.Zones {
		availableZones = append(availableZones, string(z.Name))
	}

	return fmt.Errorf("zone %s not found. Available zones: %v", zone, availableZones)
}

func registerCustomControllers(mgr ctrl.Manager, exoClient *egov3.Client, cloudProvider *exoscale.CloudProvider, instanceProvider instance.Provider, recorder events.Recorder, zone, clusterName string) error {
	if err := (&bootstraptoken.BootstrapTokenController{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create bootstrap token controller: %w", err)
	}

	if err := (&nodeclass.ExoscaleNodeClassReconciler{
		Client:         mgr.GetClient(),
		Scheme:         mgr.GetScheme(),
		ExoscaleClient: exoClient,
		Recorder:       mgr.GetEventRecorderFor("nodeclass-controller"),
		Zone:           zone,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create nodeclass controller: %w", err)
	}

	if err := (&nodeclaim.NodeClaimReconciler{
		Client:         mgr.GetClient(),
		Scheme:         mgr.GetScheme(),
		ExoscaleClient: exoClient,
		Recorder:       recorder,
		Zone:           zone,
		ClusterName:    clusterName,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create nodeclaim controller: %w", err)
	}

	if err := (&repair.NodeRepairController{
		Client:         mgr.GetClient(),
		Scheme:         mgr.GetScheme(),
		CloudProvider:  cloudProvider,
		ExoscaleClient: exoClient,
		Recorder:       mgr.GetEventRecorderFor("repair-controller"),
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create repair controller: %w", err)
	}

	if err := garbagecollection.NewController(mgr, instanceProvider); err != nil {
		return fmt.Errorf("unable to create garbage collection controller: %w", err)
	}

	return nil
}

func createPricingProvider() (pricing.Provider, error) {
	return pricing.NewStaticProvider(&pricing.ProviderOptions{
		DefaultCurrency: pricing.EUR,
	})
}

func createInstanceTypeProvider(ctx context.Context, exoClient *egov3.Client, zone string, pricingProvider pricing.Provider) (instancetype.Provider, error) {
	provider := instancetype.NewExoscaleProvider(exoClient, zone, pricingProvider)
	if err := provider.Refresh(ctx); err != nil {
		return nil, fmt.Errorf("failed to refresh instance types: %w", err)
	}
	return provider, nil
}

func createInstanceProvider(exoClient *egov3.Client, zone string, clusterName string) instance.Provider {
	return instance.NewProvider(exoClient, zone, clusterName)
}

func createUserDataProvider(kubeClient client.Client, clusterEndpoint string, clusterDNS string, clusterDomain string) userdata.Provider {
	return userdata.NewProvider(kubeClient, clusterEndpoint, clusterDNS, clusterDomain)
}
