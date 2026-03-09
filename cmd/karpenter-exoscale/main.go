package main

import (
	"context"
	"fmt"
	"os"

	egov3 "github.com/exoscale/egoscale/v3"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/cloudprovider"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/controllers/bootstraptoken"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/controllers/garbagecollection"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/controllers/nodeclass"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/providers/instance"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/providers/instancetype"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/providers/template"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/providers/userdata"
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
	options, err := instance.NewOptionsFromEnvironment(op.GetConfig().Host)
	if err != nil {
		return err
	}

	exoClient, err := options.BuildExoscaleClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to create Exoscale client: %w", err)
	}

	instanceTypeProvider, err := instancetype.NewExoscaleProvider(ctx, exoClient, options.Zone)
	if err != nil {
		return fmt.Errorf("failed to create instance type provider: %w", err)
	}

	if err := instanceTypeProvider.Refresh(ctx); err != nil {
		return fmt.Errorf("failed to refresh instance types: %w", err)
	}

	templateResolver := template.NewResolver(exoClient, options.Zone, op.GetConfig())
	userDataProvider := userdata.NewProvider(op.GetClient())
	instanceProvider := instance.NewProvider(exoClient, instanceTypeProvider, templateResolver, userDataProvider, options)

	cloudProvider := cloudprovider.NewCloudProvider(
		op.GetClient(),
		op.EventRecorder,
		instanceTypeProvider,
		instanceProvider,
	)
	decoratedCloudProvider := overlay.Decorate(cloudProvider, op.GetClient(), op.InstanceTypeStore)

	clusterState := state.NewCluster(op.Clock, op.GetClient(), decoratedCloudProvider)

	controllerList := controllers.NewControllers(
		ctxOp,
		op.Manager,
		op.Clock,
		op.GetClient(),
		op.EventRecorder,
		decoratedCloudProvider,
		cloudProvider,
		clusterState,
		op.InstanceTypeStore,
	)

	if err := registerControllers(op.Manager, exoClient, instanceProvider, templateResolver, options.Zone); err != nil {
		return fmt.Errorf("failed to register custom controllers: %w", err)
	}

	op.WithControllers(ctxOp, controllerList...).Start(ctxOp)

	return nil
}

func registerControllers(mgr ctrl.Manager, exoClient *egov3.Client, instanceProvider *instance.Provider, templateResolver *template.Provider, zone string) error {
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

	if err := garbagecollection.StartController(mgr, &garbagecollection.Controller{
		Client:           mgr.GetClient(),
		InstanceProvider: instanceProvider,
	}); err != nil {
		return fmt.Errorf("unable to create garbage collection controller: %w", err)
	}

	return nil
}
