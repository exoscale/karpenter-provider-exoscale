// Command cleanup destroys one e2e cluster by name (-name), or every e2e
// leftover older than -older-than.
package main

import (
	"context"
	"flag"
	"log"
	"time"

	"github.com/exoscale/karpenter-provider-exoscale/test/e2e/infra"
)

func main() {
	name := flag.String("name", "", "destroy the e2e resources with this name")
	olderThan := flag.Duration("older-than", 0, "destroy e2e resources older than this")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	c, err := infra.NewClient(ctx)
	if err != nil {
		log.Fatal(err)
	}

	names := []string{*name}
	if *name == "" {
		if *olderThan == 0 {
			log.Fatal("one of -name or -older-than is required")
		}
		if names, err = infra.Stale(ctx, c, *olderThan); err != nil {
			log.Fatal(err)
		}
	}

	failed := false
	for _, n := range names {
		log.Printf("destroying %s", n)
		if err := infra.Destroy(ctx, c, n); err != nil {
			log.Printf("destroy %s: %v", n, err)
			failed = true
		}
	}
	if failed {
		log.Fatal("cleanup incomplete")
	}
}
