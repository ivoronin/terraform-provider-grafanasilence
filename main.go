// Package main is the entrypoint for the Terraform provider binary.
package main

import (
	"context"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/ivoronin/terraform-provider-grafanasilence/internal/provider"
)

func main() {
	err := providerserver.Serve(context.Background(), provider.New, providerserver.ServeOpts{
		Address: "registry.terraform.io/ivoronin/grafanasilence",
	})
	if err != nil {
		log.Fatal(err)
	}
}
