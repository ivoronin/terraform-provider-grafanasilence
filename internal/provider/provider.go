// Package provider implements the Terraform provider for Grafana Alertmanager silences.
package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ivoronin/terraform-provider-grafanasilence/internal/client"
)

var _ provider.Provider = (*grafanaSilenceProvider)(nil)

type grafanaSilenceProvider struct{}

type grafanaSilenceProviderModel struct {
	URL  types.String `tfsdk:"url"`
	Auth types.String `tfsdk:"auth"`
}

// New creates a new instance of the Grafana silence provider.
func New() provider.Provider {
	return &grafanaSilenceProvider{}
}

func (p *grafanaSilenceProvider) Metadata(
	_ context.Context,
	_ provider.MetadataRequest,
	resp *provider.MetadataResponse,
) {
	resp.TypeName = "grafanasilence"
}

func (p *grafanaSilenceProvider) Schema(
	_ context.Context,
	_ provider.SchemaRequest,
	resp *provider.SchemaResponse,
) {
	resp.Schema = schema.Schema{
		Description: "Provider for managing Grafana Alertmanager silences.",
		Attributes: map[string]schema.Attribute{
			"url": schema.StringAttribute{
				Description: "Grafana instance base URL. " +
					"Can also be set with the GRAFANA_URL environment variable.",
				Optional: true,
			},
			"auth": schema.StringAttribute{
				Description: "API token or user:pass for basic auth. " +
					"Can also be set with the GRAFANA_AUTH environment variable.",
				Optional:  true,
				Sensitive: true,
			},
		},
	}
}

func (p *grafanaSilenceProvider) Configure(
	ctx context.Context,
	req provider.ConfigureRequest,
	resp *provider.ConfigureResponse,
) {
	var config grafanaSilenceProviderModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)

	if resp.Diagnostics.HasError() {
		return
	}

	url := os.Getenv("GRAFANA_URL")
	if !config.URL.IsNull() {
		url = config.URL.ValueString()
	}

	auth := os.Getenv("GRAFANA_AUTH")
	if !config.Auth.IsNull() {
		auth = config.Auth.ValueString()
	}

	if url == "" {
		resp.Diagnostics.AddError(
			"Missing Grafana URL",
			"The provider requires a Grafana URL. "+
				"Set it in the provider configuration or "+
				"via the GRAFANA_URL environment variable.",
		)
	}

	if auth == "" {
		resp.Diagnostics.AddError(
			"Missing Grafana Auth",
			"The provider requires authentication credentials. "+
				"Set them in the provider configuration or "+
				"via the GRAFANA_AUTH environment variable.",
		)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	apiClient := client.New(url, auth)
	resp.DataSourceData = apiClient
	resp.ResourceData = apiClient
}

func (p *grafanaSilenceProvider) Resources(
	_ context.Context,
) []func() resource.Resource {
	return []func() resource.Resource{
		NewSilenceResource,
	}
}

func (p *grafanaSilenceProvider) DataSources(
	_ context.Context,
) []func() datasource.DataSource {
	return nil
}
