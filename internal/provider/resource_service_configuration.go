package provider

import (
	"context"
	"encoding/base64"
	"fmt"

	"cloud.google.com/go/servicemanagement/apiv1/servicemanagementpb"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &ServiceConfigResource{}
var _ resource.ResourceWithImportState = &ServiceConfigResource{}

func NewServiceConfigResource() resource.Resource {
	return &ServiceConfigResource{}
}

// ServiceResource  defines the resource implementation.
type ServiceConfigResource struct {
	UtilsProviderConfig
}

type ServiceConfigResourceModel struct {
	Id                    types.String `tfsdk:"id"`
	ServiceName           types.String `tfsdk:"service_name"`
	ConfigYaml            types.String `tfsdk:"config_yaml"`
	ProtoDescriptorBase64 types.String `tfsdk:"proto_descriptor_base64"`
}

func (r *ServiceConfigResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_config"
}

func (r *ServiceConfigResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "A service manager service.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the config.",
				Computed:            true,
			},
			"service_name": schema.StringAttribute{
				MarkdownDescription: "The name of the service.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplaceIfConfigured(),
				},
			},
			"config_yaml": schema.StringAttribute{
				MarkdownDescription: "The service config in YAML format.",
				Required:            true,
			},
			"proto_descriptor_base64": schema.StringAttribute{
				MarkdownDescription: "The base64-encoded proto descriptor.",
				Required:            true,
				Sensitive:           true, // Not sensitive but suppress from output
			},
		},
	}
}

func (r *ServiceConfigResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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
func (r *ServiceConfigResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data ServiceConfigResourceModel

	// This will populate the data struct with the values from the plan.
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	output, err := r.createConfig(ctx, data.ServiceName.ValueString(), data.ProtoDescriptorBase64.ValueString(), data.ConfigYaml.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Could not submit configuration source", err.Error())
		return
	}

	data.Id = newConfigId(output.ServiceConfig.GetName(), output.ServiceConfig.GetId())

	// Save created data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Read implements resource.Resource.
func (r *ServiceConfigResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data ServiceConfigResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	serviceName, configId, err := parseConfigId(data.Id.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid config ID", err.Error())
		return
	}

	tflog.Debug(ctx, "Reading service config", map[string]interface{}{
		"service_name": serviceName,
		"config_id":    configId,
	})
	config, err := r.ServiceManagerClient.GetServiceConfig(ctx, &servicemanagementpb.GetServiceConfigRequest{
		ServiceName: serviceName,
		ConfigId:    configId,
		View:        servicemanagementpb.GetServiceConfigRequest_FULL,
	})

	if err != nil {
		resp.Diagnostics.AddError("Could not retrieve configuration for service", err.Error())
		return
	}

	tflog.Debug(ctx, "Retrieved service config")

	data.Id = newConfigId(config.Name, config.Id)
	data.ServiceName = types.StringValue(config.Name)

	sourceFiles := config.GetSourceInfo().GetSourceFiles()
	for _, sourceFile := range sourceFiles {
		// SourceFiles are of type google.api.servicemanagement.v1.ConfigFile
		// https://cloud.google.com/service-infrastructure/docs/service-management/reference/rest/v1/ConfigView
		var file servicemanagementpb.ConfigFile
		if err := sourceFile.UnmarshalTo(&file); err != nil {
			resp.Diagnostics.AddError("Could not unmarshal source file", err.Error())
			return
		}

		tflog.Debug(ctx, "Discovered source file", map[string]interface{}{
			"file_path": file.GetFilePath(),
			"file_type": file.GetFileType(),
		})

		switch file.FileType {
		case servicemanagementpb.ConfigFile_FILE_DESCRIPTOR_SET_PROTO:
			data.ProtoDescriptorBase64 = types.StringValue(base64.StdEncoding.EncodeToString(file.GetFileContents()))
		case servicemanagementpb.ConfigFile_SERVICE_CONFIG_YAML:
			data.ConfigYaml = types.StringValue(string(file.GetFileContents()))
		default:
			resp.Diagnostics.AddError("Unknown file type", fmt.Sprintf("Unknown file type: %v", file.FileType))
		}
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update implements resource.Resource.
func (r *ServiceConfigResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data ServiceConfigResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	output, err := r.createConfig(ctx, data.ServiceName.ValueString(), data.ProtoDescriptorBase64.ValueString(), data.ConfigYaml.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Could not submit configuration source", err.Error())
		return
	}

	data.Id = newConfigId(output.ServiceConfig.GetName(), output.ServiceConfig.GetId())

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete implements resource.Resource.
func (r *ServiceConfigResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Nothing to do
}

// ImportState implements resource.ResourceWithImportState.
func (r *ServiceConfigResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *ServiceConfigResource) createConfig(ctx context.Context, serviceName, protoDescriptor, configYaml string) (*servicemanagementpb.SubmitConfigSourceResponse, error) {
	proto, err := base64.StdEncoding.DecodeString(protoDescriptor)
	if err != nil {
		return nil, fmt.Errorf("could not decode proto descriptor: %w", err)
	}
	configOp, err := r.ServiceManagerClient.SubmitConfigSource(ctx, &servicemanagementpb.SubmitConfigSourceRequest{
		ServiceName: serviceName,
		ConfigSource: &servicemanagementpb.ConfigSource{
			Files: []*servicemanagementpb.ConfigFile{
				{
					FileContents: []byte(configYaml),
					FilePath:     "service.yaml",
					FileType:     servicemanagementpb.ConfigFile_SERVICE_CONFIG_YAML,
				},
				{
					FileContents: proto,
					FilePath:     "descriptor.pb",
					FileType:     servicemanagementpb.ConfigFile_FILE_DESCRIPTOR_SET_PROTO,
				},
			},
		},
	})

	if err != nil {
		return nil, err
	}

	config, err := configOp.Wait(ctx)
	if err != nil {
		return nil, err
	}

	return config, nil
}
