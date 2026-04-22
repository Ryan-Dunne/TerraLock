package mapper

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
)

type Block struct { // Represents a nested block within a Terraform resource, such as "ingress" in a security group, with its own type and attributes
	Type  string
	Attrs map[string]string
}

type LiveResource struct { // Represents a resource that exists in the live AWS environment, with its ID, name, attributes, and any nested blocks
	ID     string
	Name   string
	Attrs  map[string]string
	Blocks []Block
}

// Defines the interface all resource scanners must implement, including methods to fetch live resources, find missing ones compared to Terraform, and convert to HCL
type ResourceScanner interface {
	TerraformType() string
	Fetch(ctx context.Context, cfg aws.Config) ([]LiveResource, error)
	FindMissing(terraform []TerraformResource, live []LiveResource) []LiveResource
	ToHCL(resource LiveResource, label string) string
}

// Sanitizes a string to be used as a Terraform resource label by replacing invalid characters with underscores and converting to lowercase
func SanitizeResourceName(name string) string {

	var builder strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			builder.WriteRune(r)
			continue
		}
		if r == '-' || r == ' ' {
			builder.WriteRune('_')
		}
	}
	return strings.ToLower(builder.String())
}
