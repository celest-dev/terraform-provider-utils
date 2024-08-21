package provider

import (
	"context"
	"fmt"

	"cloud.google.com/go/servicemanagement/apiv1/servicemanagementpb"
	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &ServiceRolloutResource{}
var _ resource.ResourceWithImportState = &ServiceRolloutResource{}

func NewServiceRolloutResource() resource.Resource {
	return &ServiceRolloutResource{}
}

// ServiceResource  defines the resource implementation.
type ServiceRolloutResource struct {
	UtilsProviderConfig
}

type ServiceRolloutResourceModel struct {
	Id            types.String `tfsdk:"id"`
	ConfigId      types.String `tfsdk:"config_id"`
	RolloutConfig types.Map    `tfsdk:"rollout_config"`
}

func (r *ServiceRolloutResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_rollout"
}

// Schema implements resource.Resource.
func (r *ServiceRolloutResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A service manager service rollout.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the rollout.",
				Computed:            true,
			},
			"config_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the config. Only one of `config_id` or `rollout_config` can be specified.",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.ExactlyOneOf(path.MatchRoot("config_id"), path.MatchRoot("rollout_config")),
				},
			},
			"rollout_config": schema.MapAttribute{
				MarkdownDescription: "The rollout configuration by config ID. Only one of `config_id` or `rollout_config` can be specified.",
				Optional:            true,
				ElementType:         types.Float64Type,
				Validators: []validator.Map{
					mapvalidator.ExactlyOneOf(path.MatchRoot("config_id"), path.MatchRoot("rollout_config")),
				},
			},
		},
	}
}

func (r *ServiceRolloutResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	config, ok := req.ProviderData.(*UtilsProviderConfig)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *UtilsProviderConfig, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.ServiceManagerClient = config.ServiceManagerClient
	r.OperationsClient = config.OperationsClient
}

// Create implements resource.Resource.
func (r *ServiceRolloutResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data ServiceRolloutResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	rolloutId := r.createRollout(ctx, data, resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	data.Id = *rolloutId

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete implements resource.Resource.
func (r *ServiceRolloutResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// No-op. TF will remove from state, but GCP does not support deleting rollout.
}

// Read implements resource.Resource.
func (r *ServiceRolloutResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data ServiceRolloutResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.Id.IsNull() {
		return
	}

	serviceName, rolloutId, err := parseRolloutId(data.Id.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid ID", err.Error())
		return
	}

	rollout, err := r.ServiceManagerClient.GetServiceRollout(ctx, &servicemanagementpb.GetServiceRolloutRequest{
		ServiceName: serviceName,
		RolloutId:   rolloutId,
	})

	if err != nil {
		if status, ok := status.FromError(err); ok && status.Code() == codes.NotFound {
			return
		}
		resp.Diagnostics.AddError("Error reading service rollout", err.Error())
		return
	}

	rawRolloutConfig := rollout.GetTrafficPercentStrategy().GetPercentages()
	rolloutConfig, diags := types.MapValueFrom(ctx, types.Float64Type, rawRolloutConfig)
	resp.Diagnostics.Append(diags...)

	if resp.Diagnostics.HasError() {
		return
	}

	if data.ConfigId.IsNull() && data.RolloutConfig.IsNull() {
		if len(rawRolloutConfig) == 1 {
			var configId string
			for key := range rawRolloutConfig {
				configId = key
			}
			// Populate the config ID.
			data.ConfigId = newConfigId(serviceName, configId)
		} else {
			// Populate the rollout config.
			data.RolloutConfig = rolloutConfig
		}
	} else if data.ConfigId.IsNull() {
		data.RolloutConfig = rolloutConfig
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update implements resource.Resource.
func (r *ServiceRolloutResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data ServiceRolloutResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	rolloutId := r.createRollout(ctx, data, resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	data.Id = *rolloutId

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ServiceRolloutResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *ServiceRolloutResource) createRollout(ctx context.Context, data ServiceRolloutResourceModel, diagnostics diag.Diagnostics) *basetypes.StringValue {
	var serviceName string
	percentages := make(map[string]float64)

	if !data.ConfigId.IsNull() {
		svc, configId, err := parseConfigId(data.ConfigId.ValueString())
		if err != nil {
			diagnostics.AddError("Invalid config ID", err.Error())
			return nil
		}
		serviceName = svc
		percentages[configId] = 100
	} else {
		rawPercentages := make(map[string]float64)
		diags := data.RolloutConfig.ElementsAs(ctx, &rawPercentages, false)
		diagnostics.Append(diags...)
		if diagnostics.HasError() {
			return nil
		}
		for k, v := range rawPercentages {
			svcName, configId, err := parseConfigId(k)
			if err != nil {
				diagnostics.AddError("Invalid config ID", err.Error())
				return nil
			}
			if serviceName == "" {
				serviceName = svcName
			} else if serviceName != svcName {
				diagnostics.AddError("Invalid config ID", "All config IDs must be for the same service")
				return nil
			}
			percentages[configId] = v
		}
	}

	// Create the rollout.

	rolloutOp, err := r.ServiceManagerClient.CreateServiceRollout(ctx, &servicemanagementpb.CreateServiceRolloutRequest{
		ServiceName: serviceName,
		Rollout: &servicemanagementpb.Rollout{
			ServiceName: serviceName,
			Strategy: &servicemanagementpb.Rollout_TrafficPercentStrategy_{
				TrafficPercentStrategy: &servicemanagementpb.Rollout_TrafficPercentStrategy{
					Percentages: percentages,
				},
			},
		},
	})

	if err != nil {
		diagnostics.AddError("Error creating service rollout", err.Error())
		return nil
	}

	rollout, err := rolloutOp.Wait(ctx)
	if err != nil {
		diagnostics.AddError("Error creating service rollout", err.Error())
		return nil
	}

	rolloutId := newRolloutId(serviceName, rollout.RolloutId)
	return &rolloutId
}
