package mapper

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

type VPCScanner struct{}

func (s *VPCScanner) TerraformType() string { return "aws_vpc" }

func (s *VPCScanner) Fetch(ctx context.Context, cfg aws.Config) ([]LiveResource, error) {
	client := ec2.NewFromConfig(cfg)

	var resources []LiveResource
	paginator := ec2.NewDescribeVpcsPaginator(client, &ec2.DescribeVpcsInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("DescribeVpcs failed: %w", err)
		}
		for _, vpc := range output.Vpcs {
			if aws.ToBool(vpc.IsDefault) {
				continue
			}
			name := ""
			for _, tag := range vpc.Tags {
				if aws.ToString(tag.Key) == "Name" {
					name = aws.ToString(tag.Value)
					break
				}
			}
			attrs := map[string]string{
				"cidr_block": aws.ToString(vpc.CidrBlock),
			}
			resources = append(resources, LiveResource{
				ID:    aws.ToString(vpc.VpcId),
				Name:  name,
				Attrs: attrs,
			})

		}
	}
	return resources, nil
}

func (s *VPCScanner) FindMissing(terraform []TerraformResource, live []LiveResource) []LiveResource {
	known := map[string]struct{}{}
	for _, resource := range terraform {
		if resource.Type != s.TerraformType() {
			continue
		}
		if name := extractTagName(resource.Attributes["tags"]); name != "" {
			known[name] = struct{}{}
		}
	}

	var missing []LiveResource
	for _, resource := range live {
		if resource.Name == "" {
			continue
		}
		if _, exists := known[resource.Name]; !exists {
			missing = append(missing, resource)
		}
	}
	return missing
}

func (s *VPCScanner) ToHCL(r LiveResource, label string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("resource \"aws_vpc\" \"%s\" {\n", label))
	b.WriteString(fmt.Sprintf("  cidr_block = \"%s\"\n", r.Attrs["cidr_block"]))
	if r.Name != "" {
		b.WriteString("\n  tags = {\n")
		b.WriteString(fmt.Sprintf("    Name = \"%s\"\n", r.Name))
		b.WriteString("  }\n")
	}

	b.WriteString("}\n")
	return b.String()
}
