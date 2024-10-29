package main

import (
	"errors"
	"os"

	egov3 "github.com/exoscale/egoscale/v3"
	"github.com/exoscale/egoscale/v3/credentials"
	exoscale "github.com/exoscale/karpenter-exoscale/pkg/cloudprovider"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/karpenter/pkg/controllers"
	"sigs.k8s.io/karpenter/pkg/operator"
)

func main() {
	ctx, op := operator.NewOperator()

	dynClient, err := dynamic.NewForConfig(op.GetConfig())
	if err != nil {
		op.GetLogger().Error(err, "fatal startup error")
		os.Exit(1)
	}

	var (
		zone           string
		exoAPIKey      string
		exoAPISecret   string
		exoAPIEndpoint string
	)

	if zone = os.Getenv("EXOSCALE_ZONE"); zone == "" {
		op.GetLogger().Error(errors.New("no Exoscale zone specified"), "fatal startup error")
		os.Exit(1)
	}

	if exoAPIKey = os.Getenv("EXOSCALE_API_KEY"); exoAPIKey == "" {
		op.GetLogger().Error(errors.New("no Exoscale API key specified"), "fatal startup error")
	}

	if exoAPISecret = os.Getenv("EXOSCALE_API_SECRET"); exoAPISecret == "" {
		op.GetLogger().Error(errors.New("no Exoscale API secret specified"), "fatal startup error")
	}

	// permit custom API endpoint
	exoAPIEndpoint = os.Getenv("EXOSCALE_API_ENDPOINT")



	exoClient, err := egov3.NewClient(credentials.NewStaticCredentials(exoAPIKey, exoAPISecret), egov3.ClientOptWithEndpoint(
		egov3.Endpoint(exoAPIEndpoint)))
	if err != nil {
		op.GetLogger().Error(err, "fatal startup error")
		os.Exit(1)
	}

	// if no API endpoint is specified, find the right one based on zone name
	if exoAPIEndpoint == "" {
		// now go to the right zone client
		exoEndpoint, err := exoClient.GetZoneAPIEndpoint(ctx, egov3.ZoneName(zone))
		if err != nil {
			op.GetLogger().Error(err, "fatal startup error")
			os.Exit(1)
		}

		exoClient, err = egov3.NewClient(credentials.NewStaticCredentials(exoAPIKey, exoAPISecret), egov3.ClientOptWithEndpoint(
			egov3.Endpoint(exoEndpoint)))
		if err != nil {
			op.GetLogger().Error(err, "fatal startup error")
			os.Exit(1)
		}
	}

	cloudProvider := exoscale.NewCloudProvider(ctx, op.GetClient(), dynClient, exoClient, zone)
	op.
		WithControllers(ctx, controllers.NewControllers(ctx,
			op.Manager,
			op.Clock,
			op.GetClient(),
			op.EventRecorder,
			cloudProvider,
		)...).Start(ctx, cloudProvider)
}
