package provider

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ivoronin/terraform-provider-grafanasilence/internal/client"
)

var (
	_ resource.Resource                = (*silenceResource)(nil)
	_ resource.ResourceWithImportState = (*silenceResource)(nil)
)

type silenceResource struct {
	client *client.Client
}

type silenceResourceModel struct {
	ID        types.String      `tfsdk:"id"`
	StartsAt  timetypes.RFC3339 `tfsdk:"starts_at"`
	EndsAt    timetypes.RFC3339 `tfsdk:"ends_at"`
	CreatedBy types.String      `tfsdk:"created_by"`
	Comment   types.String      `tfsdk:"comment"`
	Matchers  []matcherModel    `tfsdk:"matchers"`
	Status    types.String      `tfsdk:"status"`
	UpdatedAt types.String      `tfsdk:"updated_at"`
}

type matcherModel struct {
	Name    types.String `tfsdk:"name"`
	Value   types.String `tfsdk:"value"`
	IsRegex types.Bool   `tfsdk:"is_regex"`
	IsEqual types.Bool   `tfsdk:"is_equal"`
}

// NewSilenceResource creates a new silence resource instance.
func NewSilenceResource() resource.Resource {
	return &silenceResource{}
}

func (r *silenceResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_silence"
}

func (r *silenceResource) Schema(
	_ context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = schema.Schema{
		Description: "Manages a Grafana Alertmanager silence.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "UUID assigned by the Alertmanager API.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"starts_at": schema.StringAttribute{
				Description: "Start time in RFC3339 format.",
				CustomType:  timetypes.RFC3339Type{},
				Required:    true,
				PlanModifiers: []planmodifier.String{
					replaceWhenExpired(),
				},
			},
			"ends_at": schema.StringAttribute{
				Description: "End time in RFC3339 format.",
				CustomType:  timetypes.RFC3339Type{},
				Required:    true,
				PlanModifiers: []planmodifier.String{
					replaceWhenExpired(),
				},
			},
			"created_by": schema.StringAttribute{
				Description: "Author of the silence.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					replaceWhenExpired(),
				},
			},
			"comment": schema.StringAttribute{
				Description: "Reason for the silence.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					replaceWhenExpired(),
				},
			},
			"status": schema.StringAttribute{
				Description: "Current state: active, pending, or expired.",
				Computed:    true,
			},
			"updated_at": schema.StringAttribute{
				Description: "Last updated time from API.",
				Computed:    true,
			},
		},
		Blocks: map[string]schema.Block{
			"matchers": matchersBlock(),
		},
	}
}

func matchersBlock() schema.ListNestedBlock {
	return schema.ListNestedBlock{
		Description: "Matchers that determine which alerts are silenced.",
		Validators: []validator.List{
			listvalidator.SizeAtLeast(1),
		},
		PlanModifiers: []planmodifier.List{
			replaceMatchersWhenExpired(),
		},
		NestedObject: schema.NestedBlockObject{
			Attributes: map[string]schema.Attribute{
				"name": schema.StringAttribute{
					Description: "Label name to match.",
					Required:    true,
				},
				"value": schema.StringAttribute{
					Description: "Value to match against.",
					Required:    true,
				},
				"is_regex": schema.BoolAttribute{
					Description: "Whether value is a regex pattern.",
					Required:    true,
					PlanModifiers: []planmodifier.Bool{
						boolplanmodifier.UseStateForUnknown(),
					},
				},
				"is_equal": schema.BoolAttribute{
					Description: "Whether to match for equality (true) " +
						"or inequality (false). Defaults to true.",
					Optional: true,
					Computed: true,
					Default:  booldefault.StaticBool(true),
					PlanModifiers: []planmodifier.Bool{
						boolplanmodifier.UseStateForUnknown(),
					},
				},
			},
		},
	}
}

func (r *silenceResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	if req.ProviderData == nil {
		return
	}

	apiClient, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T", req.ProviderData),
		)

		return
	}

	r.client = apiClient
}

func (r *silenceResource) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var plan silenceResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	newID, err := r.client.PostSilences(ctx, client.PostableSilence{Silence: plan.silence()})
	if err != nil {
		resp.Diagnostics.AddError("Error creating silence", err.Error())

		return
	}

	got, err := r.client.GetSilence(ctx, newID)
	if err != nil {
		resp.Diagnostics.AddError("Error reading silence after create", err.Error())

		return
	}

	plan.update(got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *silenceResource) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var state silenceResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	got, err := r.client.GetSilence(ctx, state.ID.ValueString())
	if err != nil && !errors.Is(err, client.ErrNotFound) {
		resp.Diagnostics.AddError("Error reading silence", err.Error())

		return
	}

	// Active or pending: refresh state from the API response.
	if got != nil && got.Status.State != client.SilenceStateExpired {
		state.update(got)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)

		return
	}

	// Expired or not found. If endsAt has passed, this is natural expiry:
	// keep the resource in state so Terraform doesn't try to recreate it.
	// Otherwise it was manually expired/deleted: remove so Terraform recreates.
	endsAt, parseErr := time.Parse(time.RFC3339, state.EndsAt.ValueString())
	if parseErr == nil && time.Now().After(endsAt) {
		if got != nil {
			state.update(got)
		} else {
			state.Status = types.StringValue("expired")
		}

		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)

		return
	}

	resp.State.RemoveResource(ctx)
}

func (r *silenceResource) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var plan silenceResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	var state silenceResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	postable := client.PostableSilence{
		ID:      state.ID.ValueString(),
		Silence: plan.silence(),
	}

	newID, err := r.client.PostSilences(ctx, postable)
	if err != nil {
		resp.Diagnostics.AddError("Error updating silence", err.Error())

		return
	}

	got, err := r.client.GetSilence(ctx, newID)
	if err != nil {
		resp.Diagnostics.AddError("Error reading silence after update", err.Error())

		return
	}

	plan.update(got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *silenceResource) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var state silenceResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if state.Status.ValueString() == "expired" {
		return
	}

	err := r.client.DeleteSilence(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error deleting silence", err.Error())
	}
}

func (r *silenceResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	got, err := r.client.GetSilence(ctx, req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Error importing silence", err.Error())

		return
	}

	if got.Status.State == client.SilenceStateExpired {
		resp.Diagnostics.AddError(
			"Error importing silence",
			fmt.Sprintf("Silence %s is expired", req.ID),
		)

		return
	}

	var model silenceResourceModel

	model.update(got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func (model *silenceResourceModel) silence() client.Silence {
	matchers := make([]client.Matcher, len(model.Matchers))
	for idx, matcherMdl := range model.Matchers {
		matchers[idx] = matcherMdl.matcher()
	}

	return client.Silence{
		StartsAt:  model.StartsAt.ValueString(),
		EndsAt:    model.EndsAt.ValueString(),
		CreatedBy: model.CreatedBy.ValueString(),
		Comment:   model.Comment.ValueString(),
		Matchers:  matchers,
	}
}

func (model *silenceResourceModel) update(silence *client.GettableSilence) {
	model.ID = types.StringValue(silence.ID)
	model.StartsAt = timetypes.NewRFC3339ValueMust(silence.StartsAt)
	model.EndsAt = timetypes.NewRFC3339ValueMust(silence.EndsAt)
	model.CreatedBy = types.StringValue(silence.CreatedBy)
	model.Comment = types.StringValue(silence.Comment)
	model.Status = types.StringValue(string(silence.Status.State))
	model.UpdatedAt = types.StringValue(silence.UpdatedAt)

	model.Matchers = make([]matcherModel, len(silence.Matchers))
	for idx, clientMatcher := range silence.Matchers {
		model.Matchers[idx] = newMatcherModel(clientMatcher)
	}
}

func (matcherMdl matcherModel) matcher() client.Matcher {
	clientMatcher := client.Matcher{
		Name:    matcherMdl.Name.ValueString(),
		Value:   matcherMdl.Value.ValueString(),
		IsRegex: matcherMdl.IsRegex.ValueBool(),
	}

	if !matcherMdl.IsEqual.IsNull() && !matcherMdl.IsEqual.IsUnknown() {
		isEqual := matcherMdl.IsEqual.ValueBool()
		clientMatcher.IsEqual = &isEqual
	}

	return clientMatcher
}

func newMatcherModel(matcher client.Matcher) matcherModel {
	result := matcherModel{
		Name:    types.StringValue(matcher.Name),
		Value:   types.StringValue(matcher.Value),
		IsRegex: types.BoolValue(matcher.IsRegex),
	}

	if matcher.IsEqual != nil {
		result.IsEqual = types.BoolValue(*matcher.IsEqual)
	} else {
		result.IsEqual = types.BoolValue(true)
	}

	return result
}

const expiredReplaceDescription = "Expired silences cannot be updated in place; " +
	"Grafana creates a new silence instead."

func replaceWhenExpired() planmodifier.String {
	return stringplanmodifier.RequiresReplaceIf(
		func(
			ctx context.Context,
			req planmodifier.StringRequest,
			resp *stringplanmodifier.RequiresReplaceIfFuncResponse,
		) {
			resp.RequiresReplace = isExpired(ctx, req.State, &resp.Diagnostics)
		},
		"Replace when expired",
		expiredReplaceDescription,
	)
}

func replaceMatchersWhenExpired() planmodifier.List {
	return listplanmodifier.RequiresReplaceIf(
		func(
			ctx context.Context,
			req planmodifier.ListRequest,
			resp *listplanmodifier.RequiresReplaceIfFuncResponse,
		) {
			resp.RequiresReplace = isExpired(ctx, req.State, &resp.Diagnostics)
		},
		"Replace when expired",
		expiredReplaceDescription,
	)
}

func isExpired(
	ctx context.Context,
	state tfsdk.State,
	diags *diag.Diagnostics,
) bool {
	var status types.String

	diags.Append(state.GetAttribute(ctx, path.Root("status"), &status)...)

	if diags.HasError() {
		return false
	}

	return status.ValueString() == "expired"
}
