package main

import (
	"os"

	exoscale "github.com/exoscale/karpenter-exoscale/pkg/cloudprovider"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/karpenter/pkg/controllers"
	"sigs.k8s.io/karpenter/pkg/operator"
)

var ()

func main() {
	ctx, op := operator.NewOperator()

	dynClient, err := dynamic.NewForConfig(op.GetConfig())
	if err != nil {
		os.Stderr.WriteString(err.Error())
		os.Exit(1)
	}

	cloudProvider := exoscale.NewCloudProvider(ctx, op.GetClient(), dynClient)
	op.
		WithControllers(ctx, controllers.NewControllers(ctx,
			op.Manager,
			op.Clock,
			op.GetClient(),
			op.EventRecorder,
			cloudProvider,
		)...).Start(ctx, cloudProvider)
}
