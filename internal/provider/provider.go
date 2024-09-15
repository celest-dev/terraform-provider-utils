package provider

import (
	"context"

	lrauto "cloud.google.com/go/longrunning/autogen"
	servicemanagement "cloud.google.com/go/servicemanagement/apiv1"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"golang.org/x/oauth2"
	googleoauth "golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"google.golang.org/api/serviceconsumermanagement/v1"
	"google.golang.org/grpc/credentials/oauth"
)

// Ensure UtilsProvider satisfies various provider interfaces.
var _ provider.Provider = &UtilsProvider{}
var _ provider.ProviderWithConfigValidators = &UtilsProvider{}

// scopes are the required OAuth scopes for the provider.
var scopes = []string{
	"https://www.googleapis.com/auth/cloud-platform",
	"https://www.googleapis.com/auth/service.management",
}

// UtilsProvider defines the provider implementation.
type UtilsProvider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and ran locally, and "test" when running acceptance
	// testing.
	version string
}

// UtilsProviderConfig holds the necessary GCP configuration for the provider.
type UtilsProviderConfig struct {
	// ServiceManagerClient is the authenticated client for `servicemanagement.googleapis.com`.
	ServiceManagerClient *servicemanagement.ServiceManagerClient

	// TenantClient is the authenticated client for `serviceconsumermanagement.googleapis.com`.
	TenantClient *serviceconsumermanagement.APIService

	// OperationsClient is the authenticated operations client for `servicemanagement.googleapis.com`.
	OperationsClient *lrauto.OperationsClient
}

// UtilsProviderModel describes the provider data model.
type UtilsProviderModel struct {
	// ProjectID is the GCP project to use for requests.
	ProjectID types.String `tfsdk:"project_id"`

	// Optional. AccessToken is the optional GCP access token.
	AccessToken types.String `tfsdk:"access_token"`
}

func (p *UtilsProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "utils"
	resp.Version = p.version
}

func (p *UtilsProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"project_id": schema.StringAttribute{
				MarkdownDescription: "GCP project ID",
				Optional:            true,
			},
			"access_token": schema.StringAttribute{
				MarkdownDescription: "Optional. GCP access token",
				Optional:            true,
			},
		},
	}
}

func (p *UtilsProvider) ConfigValidators(ctx context.Context) []provider.ConfigValidator {
	return []provider.ConfigValidator{}
}

func (p *UtilsProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data UtilsProviderModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Resources created here must be alive for the lifetime of the provider.
	persistentCtx := context.Background()

	dialOpts := []option.ClientOption{}
	if !data.ProjectID.IsUnknown() && !data.ProjectID.IsNull() {
		dialOpts = append(dialOpts, option.WithQuotaProject(data.ProjectID.ValueString()))
	}

	var foundGoogleCreds bool
	switch {
	case !data.AccessToken.IsUnknown() && !data.AccessToken.IsNull():
		tflog.Info(ctx, "Configuring with access token")
		dialOpts = append(dialOpts, option.WithTokenSource(&oauth.TokenSource{
			TokenSource: oauth2.StaticTokenSource(&oauth2.Token{
				AccessToken: data.AccessToken.ValueString(),
			}),
		}))
		foundGoogleCreds = true

	default:
		creds, err := googleoauth.FindDefaultCredentialsWithParams(persistentCtx, googleoauth.CredentialsParams{
			Scopes: scopes,
		})
		if err == nil {
			tflog.Info(ctx, "Configuring with default credentials")
			dialOpts = append(dialOpts, option.WithCredentials(creds))
			foundGoogleCreds = true
		} else {
			tflog.Error(ctx, "Could not find default credentials")
		}
	}

	if !foundGoogleCreds {
		return
	}

	client, err := servicemanagement.NewServiceManagerClient(persistentCtx, dialOpts...)
	if err != nil {
		resp.Diagnostics.AddError("Could not create service manager client", err.Error())
		return
	}
	tenantClient, err := serviceconsumermanagement.NewService(persistentCtx, dialOpts...)
	if err != nil {
		resp.Diagnostics.AddError("Could not create tenant client", err.Error())
		return
	}
	operations, err := lrauto.NewOperationsClient(persistentCtx, dialOpts...)
	if err != nil {
		resp.Diagnostics.AddError("Could not create operations client", err.Error())
		return
	}

	config := &UtilsProviderConfig{
		ServiceManagerClient: client,
		TenantClient:         tenantClient,
		OperationsClient:     operations,
	}
	resp.ResourceData = config
	resp.DataSourceData = config
}

func (p *UtilsProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewServiceResource,
		NewServiceConfigResource,
		NewServiceRolloutResource,
		NewServiceProjectResource,
		NewServiceTenancyUnitResource,
	}
}

func (p *UtilsProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewDartVersionsDataSource,
		NewServiceConfigDataSource,
	}
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &UtilsProvider{
			version: version,
		}
	}
}
