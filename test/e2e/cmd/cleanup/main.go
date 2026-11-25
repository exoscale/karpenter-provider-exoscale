// Command cleanup destroys one e2e run by name (-name), every run whose name
// starts with -prefix, or every e2e leftover older than -older-than.
package main

import (
	"context"
	"flag"
	"log"
	"strings"
	"time"

	"github.com/exoscale/karpenter-provider-exoscale/test/e2e/infra"
)

func main() {
	name := flag.String("name", "", "destroy the e2e resources with this name")
	prefix := flag.String("prefix", infra.NamePrefix, "destroy e2e resources whose name starts with this")
	olderThan := flag.Duration("older-than", 0, "only destroy clusters older than this")
	flag.Parse()

	if !strings.HasPrefix(*prefix, infra.NamePrefix) {
		log.Fatalf("-prefix must start with %s", infra.NamePrefix)
	}
	if *name == "" && *prefix == infra.NamePrefix && *olderThan == 0 {
		log.Fatal("refusing to destroy every e2e run: set -name, a narrower -prefix, or -older-than")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	c, err := infra.NewClient(ctx)
	if err != nil {
		log.Fatal(err)
	}

	// Destroy treats a name ending with "-" as a prefix, which also finds
	// leftovers whose cluster is already gone.
	names := []string{*name}
	switch {
	case *name != "":
	case *olderThan == 0:
		names = []string{*prefix}
	default:
		if names, err = infra.Stale(ctx, c, *prefix, *olderThan); err != nil {
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
