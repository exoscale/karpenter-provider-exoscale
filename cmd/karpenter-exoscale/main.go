package main

import (
	"context"
	"fmt"
	"os"

	egov3 "github.com/exoscale/egoscale/v3"
	"github.com/exoscale/egoscale/v3/credentials"
	exoscale "github.com/exoscale/karpenter-exoscale/pkg/cloudprovider"
	"github.com/exoscale/karpenter-exoscale/pkg/controllers/bootstraptoken"
	"github.com/exoscale/karpenter-exoscale/pkg/controllers/garbagecollection"
	"github.com/exoscale/karpenter-exoscale/pkg/controllers/nodeclass"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/instance"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/instancetype"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/template"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/userdata"
	"github.com/google/uuid"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/karpenter/pkg/cloudprovider/overlay"
	"sigs.k8s.io/karpenter/pkg/controllers"
	"sigs.k8s.io/karpenter/pkg/controllers/state"
	"sigs.k8s.io/karpenter/pkg/operator"
)

// These variables are populated via ldflags in the goreleaser config.
var (
	commit    string
	branch    string
	buildDate string
	version   string
)

func main() {
	ctx := context.Background()
	ctxOp, op := operator.NewOperator()

	op.GetLogger().V(0).Info("starting operator", "version", version, "commit", commit, "branch", branch, "buildDate", buildDate)

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

	instanceTypeProvider := instancetype.NewExoscaleProvider(exoClient, config.Zone)
	if err := instanceTypeProvider.Refresh(ctx); err != nil {
		return fmt.Errorf("failed to refresh instance types: %w", err)
	}

	templateResolver := template.NewResolver(exoClient, config.Zone, op.GetConfig())
	instanceProvider := instance.NewProvider(exoClient, config.Zone, config.ClusterID, config.InstancePrefix, instanceTypeProvider, templateResolver)

	clusterEndpoint := config.ClusterEndpoint
	if strings.IsEmpty(endpoint) {
		clusterEndpoint = op.GetConfig().Host
	}
	 
	userDataProvider := userdata.NewProvider(op.GetClient(), clusterEndpoint, config.ClusterDNS, config.ClusterDomain)

	overlayUndecoratedCloudProvider := exoscale.NewCloudProvider(
		op.GetClient(),
		exoClient,
		clusterEndpoint,
		op.EventRecorder,
		instanceTypeProvider,
		instanceProvider,
		templateResolver,
		userDataProvider,
		config.Zone,
		config.ClusterID,
		config.ClusterDNS,
		config.ClusterDomain,
		config.InstancePrefix,
	)

	cloudProvider := overlay.Decorate(overlayUndecoratedCloudProvider, op.GetClient(), op.InstanceTypeStore)

	clusterState := state.NewCluster(op.Clock, op.GetClient(), cloudProvider)

	controllerList := controllers.NewControllers(
		ctxOp,
		op.Manager,
		op.Clock,
		op.GetClient(),
		op.EventRecorder,
		cloudProvider,
		overlayUndecoratedCloudProvider,
		clusterState,
		op.InstanceTypeStore,
	)

	if err := registerCustomControllers(op.Manager, exoClient, instanceProvider, templateResolver, config.Zone); err != nil {
		return fmt.Errorf("failed to register custom controllers: %w", err)
	}

	op.WithControllers(ctxOp, controllerList...).Start(ctxOp)

	return nil
}

type Config struct {
	Zone            string
	ClusterID       string
	InstancePrefix  string
	APIKey          string
	APISecret       string
	ClusterDNS      string
	ClusterDomain   string
	ClusterEndpoint string
}

func loadConfiguration() (*Config, error) {
	config := &Config{}

	var err error
	config.Zone, err = getRequiredEnv("EXOSCALE_ZONE")
	if err != nil {
		return nil, err
	}

	config.ClusterID, err = getRequiredEnv("EXOSCALE_SKS_CLUSTER_ID")
	if err != nil {
		return nil, err
	}

	if _, err := uuid.Parse(config.ClusterID); err != nil {
		return nil, fmt.Errorf("EXOSCALE_SKS_CLUSTER_ID environment variable is not a valid UUID")
	}

	config.InstancePrefix = os.Getenv("EXOSCALE_COMPUTE_INSTANCE_PREFIX")
	if config.InstancePrefix == "" {
		config.InstancePrefix = "karpenter-"
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
	config.ClusterEndpoint = os.Getenv("CLUSTER_ENDPOINT")

	return config, nil
}

func getRequiredEnv(key string) (string, error) {
	value := os.Getenv(key)
	if value == "" {
		return "", fmt.Errorf("%s environment variable is required", key)
	}
	return value, nil
}

func getEndpoint(ctx context.Context, exoClient *egov3.Client, zone string) (*egov3.Endpoint, error) {
	if value, exists := os.LookupEnv("EXOSCALE_API_ENDPOINT"); exists {
		endpoint := egov3.Endpoint(value)
		return &endpoint, nil
	}

	if value, exists := os.LookupEnv("EXOSCALE_API_ENVIRONMENT"); exists {
		if value == "ppapi" {
			endpoint := egov3.Endpoint("https://ppapi-ch-gva-2.exoscale.com/v2")
			return &endpoint, nil
		}
	}

	endpoint, err := exoClient.GetZoneAPIEndpoint(ctx, egov3.ZoneName(zone))
	if err != nil {
		return nil, fmt.Errorf("failed to get zone endpoint for %s: %w", zone, err)
	}

	return &endpoint, nil
}

func createExoscaleClient(ctx context.Context, zone, apiKey, apiSecret string) (*egov3.Client, error) {
	exoClient, err := egov3.NewClient(
		credentials.NewStaticCredentials(apiKey, apiSecret),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Exoscale client: %w", err)
	}

	endpoint, err := getEndpoint(ctx, exoClient, zone)
	if err != nil {
		return nil, fmt.Errorf("failed to get Exoscale API endpoint: %w", err)
	}

	return egov3.NewClient(
		credentials.NewStaticCredentials(apiKey, apiSecret),
		egov3.ClientOptWithEndpoint(*endpoint),
	)
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

func registerCustomControllers(mgr ctrl.Manager, exoClient *egov3.Client, instanceProvider instance.Provider, templateResolver template.Resolver, zone string) error {
	if err := (&bootstraptoken.Controller{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("bootstrap-token-controller"),
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create bootstrap token controller: %w", err)
	}

	if err := (&nodeclass.ExoscaleNodeClassReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		ExoscaleClient:   exoClient,
		TemplateResolver: templateResolver,
		Recorder:         mgr.GetEventRecorderFor("nodeclass-controller"),
		Zone:             zone,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create nodeclass controller: %w", err)
	}

	if err := garbagecollection.NewController(mgr, instanceProvider); err != nil {
		return fmt.Errorf("unable to create garbage collection controller: %w", err)
	}

	return nil
}
