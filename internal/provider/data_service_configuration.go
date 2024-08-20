package provider

import (
	"context"
	"fmt"

	servicemanagement "cloud.google.com/go/servicemanagement/apiv1"
	"cloud.google.com/go/servicemanagement/apiv1/servicemanagementpb"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"google.golang.org/protobuf/encoding/protojson"
)

type ServiceConfigDataSource struct {
	ServiceManagerClient *servicemanagement.ServiceManagerClient
}

type ServiceConfigDataSourceModel struct {
	ID types.String `tfsdk:"id"`

	// Computed
	ServiceConfigJSON types.String `tfsdk:"service_config_json"`
}

// Metadata implements datasource.DataSource.
func (s *ServiceConfigDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_config"
}

// Schema implements datasource.DataSource.
func (s *ServiceConfigDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A service manager service configuration.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the config. Format: `{serviceName}/{configId}`.",
				Required:            true,
			},
			"service_config_json": schema.StringAttribute{
				MarkdownDescription: "The service config in JSON format.",
				Computed:            true,
			},
		},
	}
}

func (d *ServiceConfigDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

	d.ServiceManagerClient = config.ServiceManagerClient
}

// Read implements datasource.DataSource.
func (d *ServiceConfigDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data ServiceConfigDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	serviceName, configID, err := parseConfigId(data.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse config ID", err.Error())
		return
	}
	config, err := d.ServiceManagerClient.GetServiceConfig(ctx, &servicemanagementpb.GetServiceConfigRequest{
		ServiceName: serviceName,
		ConfigId:    configID,
		View:        servicemanagementpb.GetServiceConfigRequest_FULL,
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to get service config", err.Error())
		return
	}

	configJSON, err := protojson.Marshal(config)
	if err != nil {
		resp.Diagnostics.AddError("Failed to marshal service config", err.Error())
		return
	}

	data.ServiceConfigJSON = types.StringValue(string(configJSON))
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func NewServiceConfigDataSource() datasource.DataSource {
	return &ServiceConfigDataSource{}
}

var _ datasource.DataSource = &ServiceConfigDataSource{}
var _ datasource.DataSourceWithConfigure = &ServiceConfigDataSource{}
