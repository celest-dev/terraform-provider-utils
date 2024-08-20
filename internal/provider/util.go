package provider

import (
	"errors"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func parseConfigId(id string) (string, string, error) {
	parts := strings.Split(id, "/")
	if len(parts) != 2 {
		return "", "", errors.New("ID must be in the format `{serviceName}/{configId}`")
	}
	return parts[0], parts[1], nil
}

func newConfigId(serviceName, configId string) types.String {
	return types.StringValue(serviceName + "/" + configId)
}

func parseRolloutId(id string) (string, string, error) {
	parts := strings.Split(id, "/")
	if len(parts) != 2 {
		return "", "", errors.New("ID must be in the format `{serviceName}/{rolloutId}`")
	}
	return parts[0], parts[1], nil
}

func newRolloutId(serviceName, rolloutId string) types.String {
	return types.StringValue(serviceName + "/" + rolloutId)
}
