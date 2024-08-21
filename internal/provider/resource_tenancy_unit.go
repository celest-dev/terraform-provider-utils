package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"google.golang.org/api/serviceconsumermanagement/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &ServiceTenancyUnitResource{}
var _ resource.ResourceWithImportState = &ServiceTenancyUnitResource{}

func NewServiceTenancyUnitResource() resource.Resource {
	return &ServiceTenancyUnitResource{}
}

// ServiceTenancyUnitResource defines the resource implementation.
type ServiceTenancyUnitResource struct {
	UtilsProviderConfig
}

// ServiceTenancyUnitModel describes the resource data model.
type ServiceTenancyUnitModel struct {
	ID          types.String `tfsdk:"id"`
	ServiceName types.String `tfsdk:"service_name"`
	Consumer    types.String `tfsdk:"consumer"`
}

func (r *ServiceTenancyUnitResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_tenancy_unit"
}

func (r *ServiceTenancyUnitResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "A tenancy unit in a Service Manager service.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the tenancy unit.",
				Optional:            true,
				Computed:            true,
			},
			"service_name": schema.StringAttribute{
				MarkdownDescription: "The name of the service.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplaceIfConfigured(),
				},
			},
			"consumer": schema.StringAttribute{
				MarkdownDescription: "The consumer's ID, for example `projects/{project_number}`.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplaceIfConfigured(),
				},
				Validators: []validator.String{
					stringvalidator.RegexMatches(regexp.MustCompile(`^projects/\d+$`), "Consumer must be `projects/{project_number}`"),
				},
			},
		},
	}
}

func (r *ServiceTenancyUnitResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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
	r.TenantClient = clients.TenantClient
	r.OperationsClient = clients.OperationsClient
}

func (r *ServiceTenancyUnitResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data ServiceTenancyUnitModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	var id string
	if !data.ID.IsUnknown() && !data.ID.IsNull() {
		id = data.ID.ValueString()
	}

	parent := fmt.Sprintf("services/%s/%s", data.ServiceName.ValueString(), data.Consumer.ValueString())
	tenancyUnit, err := r.TenantClient.Services.TenancyUnits.Create(parent, &serviceconsumermanagement.CreateTenancyUnitRequest{
		TenancyUnitId: id,
	}).Context(ctx).Do()
	if err != nil {
		resp.Diagnostics.AddError("Error creating tenancy unit", err.Error())
		return
	}

	data.ID = types.StringValue(tenancyUnit.Name)

	// Write the updated model back to Terraform
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ServiceTenancyUnitResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data ServiceTenancyUnitModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	tenancyUnit, err := r.getTenancyUnit(ctx, data.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error getting tenancy unit", err.Error())
		return
	}

	if tenancyUnit == nil {
		return
	}

	data.ID = types.StringValue(tenancyUnit.Name)
	data.ServiceName = types.StringValue(tenancyUnit.Service)
	data.Consumer = types.StringValue(tenancyUnit.Consumer)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ServiceTenancyUnitResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	panic("Updating a service is not supported")
}

func (r *ServiceTenancyUnitResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data ServiceTenancyUnitModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	_, err := r.TenantClient.Services.TenancyUnits.Delete(data.ID.ValueString()).Context(ctx).Do()
	if err != nil {
		resp.Diagnostics.AddError("Error deleting tenancy unit", err.Error())
		return
	}
}

func (r *ServiceTenancyUnitResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (p *UtilsProviderConfig) getTenancyUnit(ctx context.Context, id string) (*serviceconsumermanagement.TenancyUnit, error) {
	parent := strings.Split(id, "/tenancyUnits/")[0]
	tenancyUnits, err := p.TenantClient.Services.TenancyUnits.List(parent).Context(ctx).Do()
	if err != nil {
		if err, ok := status.FromError(err); ok && (err.Code() == codes.NotFound || strings.Contains(err.String(), "not found")) {
			return nil, nil
		}
		return nil, err
	}

	var tenancyUnit *serviceconsumermanagement.TenancyUnit
	for _, tu := range tenancyUnits.TenancyUnits {
		if strings.EqualFold(tu.Name, id) {
			tenancyUnit = tu
			break
		}
	}

	return tenancyUnit, nil
}
