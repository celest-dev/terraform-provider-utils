package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/coreos/go-semver/semver"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"golang.org/x/sync/errgroup"
)

type DartVersionsDataSource struct{}

type DartVersionsDataSourceModel struct {
	SdkType           types.String `tfsdk:"sdk_type"`
	MinVersion        types.String `tfsdk:"min_version"`
	IncludePrerelease types.Bool   `tfsdk:"include_prerelease"`

	// Computed
	ID                types.String `tfsdk:"id"`
	Versions          types.List   `tfsdk:"versions"`
	ContainerVersions types.List   `tfsdk:"container_versions"`
}

func NewDartVersionsDataSource() datasource.DataSource {
	return &DartVersionsDataSource{}
}

// Metadata implements datasource.DataSource.
func (s *DartVersionsDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dart_versions"
}

// Schema implements datasource.DataSource.
func (s *DartVersionsDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A list of Dart SDK versions.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the config. Format: `{sdkType}/{minVersion}`.",
				Computed:            true,
			},
			"sdk_type": schema.StringAttribute{
				MarkdownDescription: "The type of SDK.",
				Required:            true,
			},
			"min_version": schema.StringAttribute{
				MarkdownDescription: "The minimum version of the SDK.",
				Required:            true,
			},
			"include_prerelease": schema.BoolAttribute{
				MarkdownDescription: "Whether to include pre-release versions.",
				Optional:            true,
			},
			"versions": schema.ListAttribute{
				MarkdownDescription: "The list of versions.",
				Computed:            true,
				ElementType:         basetypes.StringType{},
			},
			"container_versions": schema.ListAttribute{
				MarkdownDescription: "The list of container versions. This excludes patch versions except for pre-release versions.",
				Computed:            true,
				ElementType:         basetypes.StringType{},
			},
		},
	}
}

func (d *DartVersionsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
}

func (d *DartVersionsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	model := DartVersionsDataSourceModel{}
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	channels := []string{"stable", "beta", "dev"}
	eg := new(errgroup.Group)

	versionsChan := make(chan []string)

	for _, channel := range channels {
		channel := channel
		eg.Go(func() error {
			versions, err := d.listVersions(channel)
			if err != nil {
				return err
			}
			versionsChan <- versions
			return nil
		})
	}

	go func() {
		err := eg.Wait()
		if err != nil {
			resp.Diagnostics.AddError("Failed to list versions", err.Error())
		}
		close(versionsChan)
	}()

	versionsSet := make(map[string]struct{})
	for versions := range versionsChan {
		if versions == nil {
			continue
		}
		for _, version := range versions {
			versionsSet[version] = struct{}{}
		}
	}

	minVersion := semver.New(model.MinVersion.ValueString())
	includePrerelease := model.IncludePrerelease.ValueBool()
	model.IncludePrerelease = types.BoolValue(includePrerelease)

	versions := make([]*semver.Version, 0, len(versionsSet))
	for version := range versionsSet {
		semversion := semver.New(version)
		if semversion.LessThan(*minVersion) {
			continue
		}
		if semversion.PreRelease != "" && !includePrerelease {
			continue
		}
		versions = append(versions, semversion)
	}
	semver.Sort(versions)

	versionAttrs := make([]attr.Value, 0, len(versions))
	for _, version := range versions {
		versionAttrs = append(versionAttrs, types.StringValue(version.String()))
	}

	containerVersionSet := map[string]struct{}{}
	for _, version := range versions {
		if version.PreRelease != "" {
			containerVersionSet[version.String()] = struct{}{}
			continue
		}
		minorVersion := fmt.Sprintf("%d.%d", version.Major, version.Minor)
		containerVersionSet[minorVersion] = struct{}{}
	}

	containerVersions := make([]string, 0, len(containerVersionSet))
	for version := range containerVersionSet {
		containerVersions = append(containerVersions, version)
	}
	sort.Strings(containerVersions)

	containerVersionAttrs := make([]attr.Value, 0, len(containerVersions))
	for _, version := range containerVersions {
		containerVersionAttrs = append(containerVersionAttrs, types.StringValue(version))
	}

	model.ID = types.StringValue(
		fmt.Sprintf("%s/%s", model.SdkType.ValueString(), model.MinVersion.ValueString()),
	)
	versionsAttr, diags := types.ListValue(basetypes.StringType{}, versionAttrs)
	resp.Diagnostics.Append(diags...)
	model.Versions = versionsAttr

	containerVersionsAttr, diags := types.ListValue(basetypes.StringType{}, containerVersionAttrs)
	resp.Diagnostics.Append(diags...)
	model.ContainerVersions = containerVersionsAttr

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

var versionRegex = regexp.MustCompile(`\d+\.\d+\.\d+`)

func (d *DartVersionsDataSource) listVersions(channel string) ([]string, error) {
	url, _ := url.Parse("https://www.googleapis.com/storage/v1/b/dart-archive/o")
	query := url.Query()
	query.Set("prefix", fmt.Sprintf("channels/%s/release/", channel))
	query.Set("delimiter", "/")
	url.RawQuery = query.Encode()

	resp, err := http.Get(url.String())
	if err != nil {
		return nil, fmt.Errorf("failed to list %s versions: %w", channel, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to list %s versions: %s", channel, resp.Status)
	}

	var response struct {
		Prefixes []string `json:"prefixes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode %s response: %w", channel, err)
	}

	versions := make([]string, 0, len(response.Prefixes))
prefixes:
	for _, prefix := range response.Prefixes {
		parts := strings.Split(prefix, "/")
		for _, part := range parts {
			if versionRegex.Match([]byte(part)) {
				versions = append(versions, part)
				continue prefixes
			}
		}
	}

	return versions, nil
}
