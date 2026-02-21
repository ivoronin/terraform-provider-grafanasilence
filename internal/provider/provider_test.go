package provider_test

import (
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/ivoronin/terraform-provider-grafanasilence/internal/provider"
)

var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"grafanasilence": providerserver.NewProtocol6WithError(provider.New()),
}
