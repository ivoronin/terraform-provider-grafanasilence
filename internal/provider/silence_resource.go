package provider

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ivoronin/terraform-provider-grafanasilence/internal/client"
)

var (
	_ resource.Resource                = (*silenceResource)(nil)
	_ resource.ResourceWithImportState = (*silenceResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*silenceResource)(nil)
)

type silenceResource struct {
	client *client.Client
}

type silenceResourceModel struct {
	ID        types.String      `tfsdk:"id"`
	StartsAt  timetypes.RFC3339 `tfsdk:"starts_at"`
	EndsAt    timetypes.RFC3339 `tfsdk:"ends_at"`
	Duration  types.String      `tfsdk:"duration"`
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
		Attributes:  silenceAttributes(),
		Blocks: map[string]schema.Block{
			"matchers": matchersBlock(),
		},
	}
}

func silenceAttributes() map[string]schema.Attribute {
	attrs := map[string]schema.Attribute{
		"id": schema.StringAttribute{
			Description: "UUID assigned by the Alertmanager API.",
			Computed:    true,
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.UseStateForUnknown(),
			},
		},
		"created_by": schema.StringAttribute{
			Description: "Author of the silence. Defaults to \"terraform\".",
			Optional:    true,
			Computed:    true,
			Default:     stringdefault.StaticString("terraform"),
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
	}

	maps.Copy(attrs, timeAttributes())

	return attrs
}

func timeAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"starts_at": schema.StringAttribute{
			Description: "Start time in RFC3339 format. " +
				"Defaults to the current time when omitted.",
			CustomType: timetypes.RFC3339Type{},
			Optional:   true,
			Computed:   true,
			PlanModifiers: []planmodifier.String{
				replaceWhenExpired(),
				stringplanmodifier.UseStateForUnknown(),
			},
		},
		"ends_at": schema.StringAttribute{
			Description: "End time in RFC3339 format. " +
				"Exactly one of ends_at or duration must be set.",
			CustomType: timetypes.RFC3339Type{},
			Optional:   true,
			Computed:   true,
			Validators: []validator.String{
				stringvalidator.ExactlyOneOf(path.MatchRoot("duration")),
			},
			PlanModifiers: []planmodifier.String{
				replaceWhenExpired(),
			},
		},
		"duration": schema.StringAttribute{
			Description: "Duration of the silence (e.g. \"6h\", \"30m\"). " +
				"Exactly one of ends_at or duration must be set.",
			Optional: true,
			Validators: []validator.String{
				durationValidator{},
			},
			PlanModifiers: []planmodifier.String{
				replaceWhenExpired(),
			},
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

	resolveTimeDefaults(&plan, &resp.Diagnostics)

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

func (r *silenceResource) ModifyPlan(
	ctx context.Context,
	req resource.ModifyPlanRequest,
	resp *resource.ModifyPlanResponse,
) {
	// Nothing to do on destroy.
	if req.Plan.Raw.IsNull() {
		return
	}

	// Read starts_at from plan. For creates with omitted starts_at this is
	// unknown; the actual value is computed later in Create. Deferring avoids
	// the "inconsistent final plan" error caused by time.Now() drift between
	// the plan and apply-time re-plan calls.
	var startsAt timetypes.RFC3339

	resp.Diagnostics.Append(resp.Plan.GetAttribute(ctx, path.Root("starts_at"), &startsAt)...)

	if resp.Diagnostics.HasError() {
		return
	}

	computeEndsAt(ctx, req.Config, &resp.Plan, startsAt, &resp.Diagnostics)
}

// resolveTimeDefaults fills in unknown starts_at and ends_at values at create
// time. starts_at defaults to now; ends_at is computed from starts_at + duration.
func resolveTimeDefaults(plan *silenceResourceModel, diags *diag.Diagnostics) {
	if plan.StartsAt.IsUnknown() {
		plan.StartsAt = formatRFC3339(time.Now())
	}

	if !plan.EndsAt.IsUnknown() || plan.Duration.IsNull() || plan.Duration.IsUnknown() {
		return
	}

	endTime, timeDiags := addDuration(plan.StartsAt, plan.Duration.ValueString())

	diags.Append(timeDiags...)

	if !diags.HasError() {
		plan.EndsAt = endTime
	}
}

// computeEndsAt sets ends_at = starts_at + duration when duration is configured
// and ends_at is not. Skipped when starts_at is unknown (create with omitted
// starts_at); ends_at will be resolved in Create instead.
func computeEndsAt(
	ctx context.Context,
	config tfsdk.Config,
	plan *tfsdk.Plan,
	startsAt timetypes.RFC3339,
	diags *diag.Diagnostics,
) {
	if startsAt.IsNull() || startsAt.IsUnknown() {
		return
	}

	var endsAtConfig timetypes.RFC3339

	diags.Append(config.GetAttribute(ctx, path.Root("ends_at"), &endsAtConfig)...)

	if diags.HasError() || !endsAtConfig.IsNull() {
		return
	}

	var duration types.String

	diags.Append(config.GetAttribute(ctx, path.Root("duration"), &duration)...)

	if diags.HasError() || duration.IsNull() || duration.IsUnknown() {
		return
	}

	endTime, timeDiags := addDuration(startsAt, duration.ValueString())

	diags.Append(timeDiags...)

	if !diags.HasError() {
		diags.Append(plan.SetAttribute(ctx, path.Root("ends_at"), endTime)...)
	}
}

// addDuration parses a Go duration string and adds it to the given start time,
// returning the result as an RFC3339 value.
func addDuration(startsAt timetypes.RFC3339, duration string) (timetypes.RFC3339, diag.Diagnostics) {
	var diags diag.Diagnostics

	startTime, timeDiags := startsAt.ValueRFC3339Time()

	diags.Append(timeDiags...)

	if diags.HasError() {
		return timetypes.NewRFC3339Null(), diags
	}

	parsed, err := time.ParseDuration(duration)
	if err != nil {
		diags.AddAttributeError(
			path.Root("ends_at"),
			"Invalid duration",
			fmt.Sprintf("Cannot parse duration: %s", err),
		)

		return timetypes.NewRFC3339Null(), diags
	}

	endTime := formatRFC3339(startTime.Add(parsed))

	return endTime, diags
}

// formatRFC3339 converts a time.Time to a second-precision UTC RFC3339 value.
func formatRFC3339(t time.Time) timetypes.RFC3339 {
	return timetypes.NewRFC3339ValueMust(t.UTC().Format(time.RFC3339))
}

// normalizeRFC3339 re-formats an RFC3339 timestamp to second precision.
// The Grafana API may return timestamps with fractional seconds (e.g. ".000Z")
// that would cause spurious plan diffs against provider-computed values.
func normalizeRFC3339(timestamp string) timetypes.RFC3339 {
	parsed, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return timetypes.NewRFC3339ValueMust(timestamp)
	}

	return formatRFC3339(parsed)
}

func (model *silenceResourceModel) silence() client.Silence {
	matchers := make([]client.Matcher, len(model.Matchers))
	for idx, mdl := range model.Matchers {
		matchers[idx] = mdl.matcher()
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
	model.StartsAt = normalizeRFC3339(silence.StartsAt)
	model.EndsAt = normalizeRFC3339(silence.EndsAt)
	model.CreatedBy = types.StringValue(silence.CreatedBy)
	model.Comment = types.StringValue(silence.Comment)
	model.Status = types.StringValue(string(silence.Status.State))
	model.UpdatedAt = types.StringValue(silence.UpdatedAt)

	model.Matchers = make([]matcherModel, len(silence.Matchers))
	for idx, clientMatcher := range silence.Matchers {
		model.Matchers[idx] = newMatcherModel(clientMatcher)
	}
}

func (m matcherModel) matcher() client.Matcher {
	clientMatcher := client.Matcher{
		Name:    m.Name.ValueString(),
		Value:   m.Value.ValueString(),
		IsRegex: m.IsRegex.ValueBool(),
	}

	if !m.IsEqual.IsNull() && !m.IsEqual.IsUnknown() {
		isEqual := m.IsEqual.ValueBool()
		clientMatcher.IsEqual = &isEqual
	}

	return clientMatcher
}

func newMatcherModel(matcher client.Matcher) matcherModel {
	model := matcherModel{
		Name:    types.StringValue(matcher.Name),
		Value:   types.StringValue(matcher.Value),
		IsRegex: types.BoolValue(matcher.IsRegex),
	}

	if matcher.IsEqual != nil {
		model.IsEqual = types.BoolValue(*matcher.IsEqual)
	} else {
		model.IsEqual = types.BoolValue(true)
	}

	return model
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

// durationValidator validates that a string is a valid, positive Go duration.
type durationValidator struct{}

func (v durationValidator) Description(_ context.Context) string {
	return "value must be a valid positive duration (e.g. \"6h\", \"30m\", \"1h30m\")"
}

func (v durationValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v durationValidator) ValidateString(
	_ context.Context,
	req validator.StringRequest,
	resp *validator.StringResponse,
) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	parsed, err := time.ParseDuration(req.ConfigValue.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid duration",
			fmt.Sprintf("Cannot parse %q as a duration: %s", req.ConfigValue.ValueString(), err),
		)

		return
	}

	if parsed <= 0 {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid duration",
			"Duration must be positive",
		)
	}
}
