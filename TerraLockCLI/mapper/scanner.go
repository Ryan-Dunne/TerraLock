package mapper

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
)

type Block struct {
	Type  string
	Attrs map[string]string
}



type LiveResource struct {
	ID     string
	Name   string
	Attrs  map[string]string
	Blocks []Block
}


type ResourceScanner interface {
	TerraformType() string
	Fetch(ctx context.Context, cfg aws.Config) ([]LiveResource, error)
	FindMissing(terraform []TerraformResource, live []LiveResource) []LiveResource
	ToHCL(resource LiveResource, label string) string
}


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
