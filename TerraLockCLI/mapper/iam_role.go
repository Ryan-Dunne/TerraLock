package mapper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
)

type IAMRoleScanner struct{}

// Fetches live IAM Roles from AWS, returns slice of LiveResources w/ attributes extracted from each role.
func (s *IAMRoleScanner) TerraformType() string { return "aws_iam_role" }

func (s *IAMRoleScanner) Fetch(ctx context.Context, cfg aws.Config) ([]LiveResource, error) {
	client := iam.NewFromConfig(cfg)

	var resources []LiveResource
	paginator := iam.NewListRolesPaginator(client, &iam.ListRolesInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("ListRoles failed: %w", err)
		}
		for _, role := range page.Roles {
			attrs := map[string]string{
				"name": aws.ToString(role.RoleName),
			}
			if role.Description != nil && *role.Description != "" {
				attrs["description"] = aws.ToString(role.Description)
			}
			if role.AssumeRolePolicyDocument != nil {
				decoded, err := url.QueryUnescape(aws.ToString(role.AssumeRolePolicyDocument))
				if err != nil {
					decoded = aws.ToString(role.AssumeRolePolicyDocument)
				}
				var pretty bytes.Buffer
				if err := json.Indent(&pretty, []byte(decoded), "  ", "  "); err == nil {
					decoded = pretty.String()
				}
				attrs["assume_role_policy"] = decoded
			}
			if role.MaxSessionDuration != nil {
				attrs["max_session_duration"] = fmt.Sprintf("%d", aws.ToInt32(role.MaxSessionDuration))
			}

			resources = append(resources, LiveResource{
				ID:    aws.ToString(role.RoleId),
				Name:  aws.ToString(role.RoleName),
				Attrs: attrs,
			})
		}
	}
	return resources, nil
}

// Compares live IAM Roles to those declared in Terraform, returns slice of LiveResources that exist in AWS but not in Terraform, uses "name" attribute
func (s *IAMRoleScanner) FindMissing(terraform []TerraformResource, live []LiveResource) []LiveResource {
	known := map[string]struct{}{}
	for _, resource := range terraform {
		if resource.Type != s.TerraformType() {
			continue
		}
		if name, ok := resource.Attributes["name"]; ok {
			known[strings.Trim(name, "\"")] = struct{}{}
		}
	}

	var missing []LiveResource
	for _, r := range live {
		if strings.HasPrefix(r.Name, "AWSServiceRole") {
			continue // Skips Aws Service roles
		}
		if _, exists := known[r.Name]; !exists {
			missing = append(missing, r)
		}
	}
	return missing
}

// Converts a LiveResource representing an IAM Role into a Terraform HCL block string, using a provided label for the resource name
func (s *IAMRoleScanner) ToHCL(r LiveResource, label string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("\nresource \"aws_iam_role\" \"%s\" {\n", label))
	for _, key := range []string{"name", "description"} {
		if v, ok := r.Attrs[key]; ok && v != "" {
			b.WriteString(fmt.Sprintf("  %s = \"%s\"\n", key, v))
		}
	}
	if v, ok := r.Attrs["max_session_duration"]; ok && v != "" {
		b.WriteString(fmt.Sprintf("  max_session_duration = %s\n", v))
	}
	if policy, ok := r.Attrs["assume_role_policy"]; ok && policy != "" {
		b.WriteString(fmt.Sprintf("  assume_role_policy = <<POLICY\n%s\nPOLICY\n", policy))
	}
	b.WriteString("}\n")
	return b.String()
}
