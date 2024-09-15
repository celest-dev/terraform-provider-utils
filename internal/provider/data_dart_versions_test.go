package provider

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

func TestAccDataSourceDartVersions(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccCreateConfig(`
				data "utils_dart_versions" "test" {
					sdk_type = "dart"
					min_version = "3.5.0"
				}`),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.utils_dart_versions.test", tfjsonpath.New("id"), knownvalue.StringExact("dart/3.5.0")),
					statecheck.ExpectKnownValue("data.utils_dart_versions.test", tfjsonpath.New("versions"), listNotEmpty{}),
					statecheck.ExpectKnownValue("data.utils_dart_versions.test", tfjsonpath.New("versions"), listEquals{[]string{"3.5.0", "3.5.1", "3.5.2", "3.5.3"}}),
				},
			},
		},
	})
}

func TestAccDataSourceDartVersionsBeta(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccCreateConfig(`
				data "utils_dart_versions" "test" {
					sdk_type = "dart"
					min_version = "3.5.0"
					channels = ["stable", "beta"]
				}`),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.utils_dart_versions.test", tfjsonpath.New("id"), knownvalue.StringExact("dart/3.5.0")),
					statecheck.ExpectKnownValue("data.utils_dart_versions.test", tfjsonpath.New("versions"), listOfGreaterThan{4}),
				},
			},
		},
	})
}

func TestAccDataSourceDartVersionsBad(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccCreateConfig(`
				data "utils_dart_versions" "beta" {
					sdk_type = "dart"
					min_version = "3.5.0"
					channels = ["stable", "bad"]
				}`),
				ExpectError: regexp.MustCompile(`must be one of`),
			},
		},
	})
}
