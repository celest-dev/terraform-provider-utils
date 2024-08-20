package provider

import (
	"context"
	"fmt"
	"strings"

	"cloud.google.com/go/servicemanagement/apiv1/servicemanagementpb"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &ServiceResource{}
var _ resource.ResourceWithImportState = &ServiceResource{}

func NewServiceResource() resource.Resource {
	return &ServiceResource{}
}

// ServiceResource  defines the resource implementation.
type ServiceResource struct {
	UtilsProviderConfig
}

// ServiceResource Model describes the resource data model.
type ServiceResourceModel struct {
	ServiceName        types.String `tfsdk:"service_name"`
	ProducerProjectId  types.String `tfsdk:"producer_project_id"`
	DefaultTenancyUnit types.String `tfsdk:"default_tenancy_unit"`
}

func (r *ServiceResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service"
}

func (r *ServiceResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "A service manager service.",

		Attributes: map[string]schema.Attribute{
			"service_name": schema.StringAttribute{
				MarkdownDescription: "The name of the service.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplaceIfConfigured(),
				},
			},
			"producer_project_id": schema.StringAttribute{
				MarkdownDescription: "The producer project id.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplaceIfConfigured(),
				},
			},
			"default_tenancy_unit": schema.StringAttribute{
				MarkdownDescription: "The tenancy unit assigned to the producer project which holds consumer projects/resources not yet assigned to Celest users.",
				Computed:            true,
			},
		},
	}
}

func (r *ServiceResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured.
	if req.ProviderData == nil {
		return
	}

	clients, ok := req.ProviderData.(*UtilsProviderConfig)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *UtilsProviderConfig, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	r.ServiceManagerClient = clients.ServiceManagerClient
	r.OperationsClient = clients.OperationsClient
}

func (r *ServiceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data ServiceResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	_, err := r.ServiceManagerClient.GetService(ctx, &servicemanagementpb.GetServiceRequest{
		ServiceName: data.ServiceName.ValueString(),
	})

	if err == nil {
		resp.Diagnostics.AddError("Service already exists", fmt.Sprintf("Service %s already exists", data.ServiceName.ValueString()))
		return
	} else if status.Code(err) != codes.NotFound && !strings.Contains(err.Error(), "not found") {
		resp.Diagnostics.AddError("Error getting service", err.Error())
		return
	}

	serviceOp, err := r.ServiceManagerClient.CreateService(ctx, &servicemanagementpb.CreateServiceRequest{
		Service: &servicemanagementpb.ManagedService{
			ServiceName:       data.ServiceName.ValueString(),
			ProducerProjectId: data.ProducerProjectId.ValueString(),
		},
	})

	if err != nil {
		resp.Diagnostics.AddError("Error creating service", err.Error())
		return
	}

	service, err := serviceOp.Wait(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error creating service", err.Error())
		return
	}

	data.ServiceName = types.StringValue(service.ServiceName)
	data.ProducerProjectId = types.StringValue(service.ProducerProjectId)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ServiceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data ServiceResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	service, err := r.ServiceManagerClient.GetService(ctx, &servicemanagementpb.GetServiceRequest{
		ServiceName: data.ServiceName.ValueString(),
	})

	if err != nil {
		if err, ok := status.FromError(err); ok && (err.Code() == codes.NotFound || strings.Contains(err.String(), "not found")) {
			return
		}
		resp.Diagnostics.AddError("Could not retrieve service", err.Error())
		return
	}

	data.ServiceName = types.StringValue(service.ServiceName)
	data.ProducerProjectId = types.StringValue(service.ProducerProjectId)

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ServiceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	panic("Updating a service is not supported")
}

func (r *ServiceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data ServiceResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	op, err := r.ServiceManagerClient.DeleteService(ctx, &servicemanagementpb.DeleteServiceRequest{
		ServiceName: data.ServiceName.ValueString(),
	})

	if err != nil {
		resp.Diagnostics.AddError("Error deleting service", err.Error())
		return
	}

	if err := op.Wait(ctx); err != nil {
		resp.Diagnostics.AddError("Error deleting service", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &ServiceResourceModel{})...)
}

func (r *ServiceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("service_name"), req, resp)
}
