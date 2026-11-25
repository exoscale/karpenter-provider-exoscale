// Command versions prints the Kubernetes versions SKS supports as a JSON array, for the CI matrix.
package main

import (
	"context"
	"encoding/json"
	"log"
	"os"

	"github.com/exoscale/karpenter-provider-exoscale/test/e2e/infra"
)

func main() {
	ctx := context.Background()
	c, err := infra.NewClient(ctx)
	if err != nil {
		log.Fatal(err)
	}
	versions, err := infra.Versions(ctx, c)
	if err != nil {
		log.Fatal(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(versions); err != nil {
		log.Fatal(err)
	}
}
