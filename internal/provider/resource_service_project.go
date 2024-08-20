package provider

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"google.golang.org/api/serviceconsumermanagement/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &ServiceProjectResource{}

func NewServiceProjectResource() resource.Resource {
	return &ServiceProjectResource{}
}

// ServiceProjectResource defines the resource implementation.
type ServiceProjectResource struct {
	UtilsProviderConfig
}

// ServiceProjectResourceModel describes the resource data model.
type ServiceProjectResourceModel struct {
	ID            types.String `tfsdk:"id"`
	TenancyUnit   types.String `tfsdk:"tenancy_unit"`
	Tag           types.String `tfsdk:"tag"`
	ProjectConfig types.Object `tfsdk:"project_config"`

	// Computed
	Status types.String `tfsdk:"status"`
}

type ServiceProjectConfigModel struct {
	Folder               types.String `tfsdk:"folder"`
	TenantProjectPolicy  types.Object `tfsdk:"tenant_project_policy"`
	Labels               types.Map    `tfsdk:"labels"`
	Services             types.List   `tfsdk:"services"`
	BillingConfig        types.Object `tfsdk:"billing_config"`
	ServiceAccountConfig types.Object `tfsdk:"service_account_config"`
}

func (ServiceProjectConfigModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"folder": types.StringType,
		"tenant_project_policy": types.ObjectType{
			AttrTypes: ServiceProjectConfigTenantProjectPolicyModel{}.AttributeTypes(),
		},
		"labels":   types.MapType{ElemType: types.StringType},
		"services": types.ListType{ElemType: types.StringType},
		"billing_config": types.ObjectType{
			AttrTypes: ServiceProjectConfigBillingConfigModel{}.AttributeTypes(),
		},
		"service_account_config": types.ObjectType{
			AttrTypes: ServiceProjectConfigServiceAccountConfigModel{}.AttributeTypes(),
		},
	}
}

type ServiceProjectConfigTenantProjectPolicyModel struct {
	PolicyBindings types.List `tfsdk:"policy_bindings"`
}

func (ServiceProjectConfigTenantProjectPolicyModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"policy_bindings": types.ListType{
			ElemType: types.ObjectType{AttrTypes: PolicyBinding{}.AttributeTypes()},
		},
	}
}

type PolicyBinding struct {
	Role    types.String `tfsdk:"role"`
	Members types.List   `tfsdk:"members"`
}

func (PolicyBinding) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"role":    types.StringType,
		"members": types.ListType{ElemType: types.StringType},
	}
}

type ServiceProjectConfigBillingConfigModel struct {
	BillingAccount types.String `tfsdk:"billing_account"`
}

func (ServiceProjectConfigBillingConfigModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"billing_account": types.StringType,
	}
}

type ServiceProjectConfigServiceAccountConfigModel struct {
	AccountID          types.String `tfsdk:"account_id"`
	TenantProjectRoles types.List   `tfsdk:"tenant_project_roles"`
}

func (ServiceProjectConfigServiceAccountConfigModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"account_id":           types.StringType,
		"tenant_project_roles": types.ListType{ElemType: types.StringType},
	}
}

func (r *ServiceProjectResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_project"
}

func (r *ServiceProjectResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "A service manager service.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the project.",
				Computed:            true,
			},
			"tenancy_unit": schema.StringAttribute{
				MarkdownDescription: "The tenancy unit the project belongs to.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(regexp.MustCompile("^services/[^/]+/[^/]+/[^/]+/tenancyUnits/[^/]+$"), "The tenancy unit must be in the format `services/{service_name}/{collection_id}/{resource_id}/tenancyUnits/{tenancy_unit_id}`."),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplaceIfConfigured(),
				},
			},
			"tag": schema.StringAttribute{
				MarkdownDescription: "The tag to apply to the project.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplaceIfConfigured(),
				},
			},
			"project_config": schema.SingleNestedAttribute{
				MarkdownDescription: "The project configuration.",
				Required:            true,
				Attributes: map[string]schema.Attribute{
					"folder": schema.StringAttribute{
						MarkdownDescription: "Folder where project in this tenancy unit must be located This folder must have been previously created with the required permissions for the caller to create and configure a project in it. Valid folder resource names have the format folders/{folder_number} (for example, folders/123456).",
						Required:            true,
					},
					"tenant_project_policy": schema.SingleNestedAttribute{
						MarkdownDescription: "Describes ownership and policies for the new tenant project. Required.",
						Required:            true,
						Attributes: map[string]schema.Attribute{
							"policy_bindings": schema.ListNestedAttribute{
								MarkdownDescription: "Policy bindings to be applied to the tenant project, in addition to the 'roles/owner' role granted to the Service Consumer Management service account. At least one binding must have the role roles/owner. Among the list of members for roles/owner, at least one of them must be either the user or group type.",
								Required:            true,
								NestedObject: schema.NestedAttributeObject{
									Attributes: map[string]schema.Attribute{
										"role": schema.StringAttribute{
											MarkdownDescription: "The role to which members will be added.",
											Required:            true,
										},
										"members": schema.ListAttribute{
											MarkdownDescription: "The members to add to the role.",
											Required:            true,
											ElementType:         types.StringType,
										},
									},
								},
							},
						},
					},
					"labels": schema.MapAttribute{
						MarkdownDescription: "Labels to apply to the project.",
						Optional:            true,
						ElementType:         types.StringType,
					},
					"services": schema.ListAttribute{
						MarkdownDescription: "Google Cloud API names of services that are activated on this project during provisioning. If any of these services can't be activated, the request fails. For example: 'compute.googleapis.com','cloudfunctions.googleapis.com'",
						Optional:            true,
						ElementType:         types.StringType,
					},
					"billing_config": schema.SingleNestedAttribute{
						MarkdownDescription: "Billing account properties. The billing account must be specified.",
						Required:            true,
						Attributes: map[string]schema.Attribute{
							"billing_account": schema.StringAttribute{
								MarkdownDescription: "Name of the billing account. For example billingAccounts/012345-567890-ABCDEF.",
								Required:            true,
							},
						},
					},
					"service_account_config": schema.SingleNestedAttribute{
						MarkdownDescription: "Configuration for the IAM service account on the tenant project.",
						Required:            true,
						Attributes: map[string]schema.Attribute{
							"account_id": schema.StringAttribute{
								MarkdownDescription: "ID of the IAM service account to be created in tenant project. The email format of the service account is \"@.iam.gserviceaccount.com\". This account ID must be unique within tenant project and service producers have to guarantee it. The ID must be 6-30 characters long, and match the following regular expression: [a-z]([-a-z0-9]*[a-z0-9]).",
								Required:            true,
								Validators: []validator.String{
									stringvalidator.RegexMatches(regexp.MustCompile("^[a-z]([-a-z0-9]*[a-z0-9])$"), "The account ID must be 6-30 characters long and match the regular expression [a-z]([-a-z0-9]*[a-z0-9])."),
								},
							},
							"tenant_project_roles": schema.ListAttribute{
								MarkdownDescription: "Roles for the associated service account for the tenant project.",
								Required:            true,
								ElementType:         types.StringType,
							},
						},
					},
				},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: `
Status: Status of tenant resource.

Possible values:
  "STATUS_UNSPECIFIED" - Unspecified status is the default unset value.
  "PENDING_CREATE" - Creation of the tenant resource is ongoing.
  "ACTIVE" - Active resource.
  "PENDING_DELETE" - Deletion of the resource is ongoing.
  "FAILED" - Tenant resource creation or deletion has failed.
  "DELETED" - Tenant resource has been deleted.`,
				Computed: true,
			},
		},
	}
}

func (r *ServiceProjectResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ServiceProjectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data ServiceProjectResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	var projectConfigModel ServiceProjectConfigModel
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("project_config"), &projectConfigModel)...)
	if resp.Diagnostics.HasError() {
		return
	}

	projectConfig := projectConfigModel.toProjectConfig(ctx, resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	parent := data.TenancyUnit.ValueString()
	op, err := r.TenantClient.Services.TenancyUnits.AddProject(parent, &serviceconsumermanagement.AddTenantProjectRequest{
		Tag:           data.Tag.ValueString(),
		ProjectConfig: projectConfig,
	}).Context(ctx).Do()

	if err != nil {
		resp.Diagnostics.AddError("Error adding project", err.Error())
		return
	}

	for !op.Done {
		time.Sleep(5 * time.Second)

		op, err = r.TenantClient.Operations.Get(op.Name).Context(ctx).Do()
		if err != nil {
			resp.Diagnostics.AddError("Error getting operation", err.Error())
			return
		}
	}

	project, err := r.getTenantProject(ctx, data.TenancyUnit.ValueString(), data.Tag.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error getting project", err.Error())
		return
	}
	if project == nil {
		panic("project not found")
	}

	data.ID = types.StringValue(project.Resource)
	data.Status = types.StringValue(project.Status)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (projectConfigModel ServiceProjectConfigModel) toProjectConfig(ctx context.Context, diags diag.Diagnostics) *serviceconsumermanagement.TenantProjectConfig {
	var tenantProjectPolicy serviceconsumermanagement.TenantProjectPolicy
	if !projectConfigModel.TenantProjectPolicy.IsUnknown() && !projectConfigModel.TenantProjectPolicy.IsNull() {
		var tenantProjectPolicyModel ServiceProjectConfigTenantProjectPolicyModel
		diags.Append(projectConfigModel.TenantProjectPolicy.As(ctx, &tenantProjectPolicyModel, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return nil
		}
		policyBindingsValue, diags := tenantProjectPolicyModel.PolicyBindings.ToListValue(ctx)
		if diags.HasError() {
			diags.Append(diags...)
			return nil
		}
		policyBindings := make([]PolicyBinding, len(policyBindingsValue.Elements()))
		diags.Append(policyBindingsValue.ElementsAs(ctx, &policyBindings, false)...)
		if diags.HasError() {
			return nil
		}
		tenantProjectPolicy.PolicyBindings = make([]*serviceconsumermanagement.PolicyBinding, len(policyBindings))
		for i, policyBinding := range policyBindings {
			var members []string
			diags.Append(policyBinding.Members.ElementsAs(ctx, &members, false)...)
			if diags.HasError() {
				return nil
			}
			tenantProjectPolicy.PolicyBindings[i] = &serviceconsumermanagement.PolicyBinding{
				Role:    policyBinding.Role.ValueString(),
				Members: members,
			}
		}
	}

	var labels map[string]string
	diags.Append(projectConfigModel.Labels.ElementsAs(ctx, &labels, false)...)
	if diags.HasError() {
		return nil
	}

	var services []string
	diags.Append(projectConfigModel.Services.ElementsAs(ctx, &services, false)...)
	if diags.HasError() {
		return nil
	}

	var billingConfig serviceconsumermanagement.BillingConfig
	if !projectConfigModel.BillingConfig.IsUnknown() && !projectConfigModel.BillingConfig.IsNull() {
		var billingConfigModel ServiceProjectConfigBillingConfigModel
		diags.Append(projectConfigModel.BillingConfig.As(ctx, &billingConfigModel, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return nil
		}
		billingConfig.BillingAccount = billingConfigModel.BillingAccount.ValueString()
	}

	var serviceAccountConfig serviceconsumermanagement.ServiceAccountConfig
	if !projectConfigModel.ServiceAccountConfig.IsUnknown() && !projectConfigModel.ServiceAccountConfig.IsNull() {
		var serviceAccountConfigModel ServiceProjectConfigServiceAccountConfigModel
		diags.Append(projectConfigModel.ServiceAccountConfig.As(ctx, &serviceAccountConfigModel, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return nil
		}
		serviceAccountConfig.AccountId = serviceAccountConfigModel.AccountID.ValueString()
		tenantProjectRolesValue, diags := serviceAccountConfigModel.TenantProjectRoles.ToListValue(ctx)
		if diags.HasError() {
			diags.Append(diags...)
			return nil
		}
		tenantProjectRoles := make([]string, len(tenantProjectRolesValue.Elements()))
		diags.Append(tenantProjectRolesValue.ElementsAs(ctx, &tenantProjectRoles, false)...)
		if diags.HasError() {
			return nil
		}
		serviceAccountConfig.TenantProjectRoles = tenantProjectRoles
	}

	return &serviceconsumermanagement.TenantProjectConfig{
		Folder:               projectConfigModel.Folder.ValueString(),
		TenantProjectPolicy:  &tenantProjectPolicy,
		Labels:               labels,
		Services:             services,
		BillingConfig:        &billingConfig,
		ServiceAccountConfig: &serviceAccountConfig,
	}
}

func (r *ServiceProjectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data ServiceProjectResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	project, err := r.getTenantProject(ctx, data.TenancyUnit.ValueString(), data.Tag.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error getting project", err.Error())
		return
	}
	if project == nil {
		return
	}

	data.ID = types.StringValue(project.Resource)
	data.Status = types.StringValue(project.Status)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ServiceProjectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data ServiceProjectResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	var projectConfigModel ServiceProjectConfigModel
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("project_config"), &projectConfigModel)...)
	if resp.Diagnostics.HasError() {
		return
	}

	projectConfig := projectConfigModel.toProjectConfig(ctx, resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	op, err := r.TenantClient.Services.TenancyUnits.ApplyProjectConfig(data.TenancyUnit.ValueString(), &serviceconsumermanagement.ApplyTenantProjectConfigRequest{
		Tag:           data.Tag.ValueString(),
		ProjectConfig: projectConfig,
	}).Context(ctx).Do()

	if err != nil {
		resp.Diagnostics.AddError("Error updating project", err.Error())
		return
	}

	for !op.Done {
		time.Sleep(5 * time.Second)

		op, err = r.TenantClient.Operations.Get(op.Name).Context(ctx).Do()
		if err != nil {
			resp.Diagnostics.AddError("Error getting operation", err.Error())
			return
		}
	}

	project, err := r.getTenantProject(ctx, data.TenancyUnit.ValueString(), data.Tag.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error getting project", err.Error())
		return
	}
	if project == nil {
		panic("project not found")
	}

	data.ID = types.StringValue(project.Resource)
	data.Status = types.StringValue(project.Status)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)

}

func (r *ServiceProjectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data ServiceProjectResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	op, err := r.TenantClient.Services.TenancyUnits.RemoveProject(data.TenancyUnit.ValueString(), &serviceconsumermanagement.RemoveTenantProjectRequest{
		Tag: data.Tag.ValueString(),
	}).Context(ctx).Do()

	if err != nil {
		resp.Diagnostics.AddError("Error removing project", err.Error())
		return
	}

	for !op.Done {
		time.Sleep(5 * time.Second)

		op, err = r.TenantClient.Operations.Get(op.Name).Context(ctx).Do()
		if err != nil {
			resp.Diagnostics.AddError("Error getting operation", err.Error())
			return
		}
	}
}

type TenantResource serviceconsumermanagement.TenantResource

func (r TenantResource) ServiceAccountEmail() string {
	resourceParts := strings.Split(r.Resource, "/") // projects/{project_id}
	if resourceParts[0] != "projects" {
		log.Panicf("unexpected resource type: %q", r.Resource)
	}
	return fmt.Sprintf("%s@%s.iam.gserviceaccount.com", r.Tag, resourceParts[1])
}

func (r *UtilsProviderConfig) getTenantProject(ctx context.Context, tenancyUnitID, tag string) (*TenantResource, error) {
	tenancyUnit, err := r.getTenancyUnit(ctx, tenancyUnitID)
	if err != nil {
		if s, ok := status.FromError(err); ok && s.Code() == codes.NotFound || strings.Contains(err.Error(), "not found") {
			return nil, nil
		}
		return nil, err
	}
	if tenancyUnit == nil {
		return nil, nil
	}
	for _, resource := range tenancyUnit.TenantResources {
		if resource.Tag == tag {
			return (*TenantResource)(resource), nil
		}
	}
	return nil, nil
}
