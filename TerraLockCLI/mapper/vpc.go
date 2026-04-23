package mapper

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

type VPCScanner struct{}

// TerraformType returns the Terraform resource type for VPCs
func (s *VPCScanner) TerraformType() string { return "aws_vpc" }

// Fetches live VPCs from AWS, returns slice of LiveResources with relevant attributes extracted from each VPC
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
			tags := map[string]string{}
			for _, tag := range vpc.Tags { //Extract tags from VPCs,Looks for "Name" tag to use as resource name
				k := aws.ToString(tag.Key)
				v := aws.ToString(tag.Value)
				tags[k] = v
				if k == "Name" {
					name = v
				}
			}
			attrs := map[string]string{
				"cidr_block":       aws.ToString(vpc.CidrBlock),
				"instance_tenancy": string(vpc.InstanceTenancy),
			}
			resources = append(resources, LiveResource{
				ID:    aws.ToString(vpc.VpcId),
				Name:  name,
				Attrs: attrs,
				Tags:  tags,
			})

		}
	}
	return resources, nil
}

// Returns a slice of LiveResources representing VPCs that exist in AWS but not in Terraform, uses "name" attribute for comparison
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
		if _, exists := known[resource.Name]; !exists { //If the Name tag value of live VPC doesn't exist in the known map built from Terraform resources, it's missing from IaC
			missing = append(missing, resource)
		}
	}
	return missing
}

// Compares live VPCs to Terraform resources by Name tag, returns tag value discrepancies
func (s *VPCScanner) FindTagDrift(terraform []TerraformResource, live []LiveResource) []TagDrift {
	return findTagDriftByNameTag(terraform, live, s.TerraformType())
}

// Compares live VPCs to Terraform resources, returns attribute value discrepancies for cidr_block
func (s *VPCScanner) FindAttrDrift(terraform []TerraformResource, live []LiveResource) []AttrDrift {
	comparable := []string{"cidr_block", "instance_tenancy"}

	tfByName := map[string]map[string]string{}
	for _, r := range terraform {
		if r.Type != s.TerraformType() {
			continue
		}
		if name := extractTagName(r.Attributes["tags"]); name != "" {
			tfByName[name] = r.Attributes
		}
	}

	var drifts []AttrDrift
	for _, r := range live {
		tfAttrs, ok := tfByName[r.Name]
		if !ok {
			continue
		}
		for _, key := range comparable { // Only compare attributes that are relevant for VPCs
			liveVal, hasLive := r.Attrs[key]
			tfRaw, hasTF := tfAttrs[key]
			if !hasLive || !hasTF {
				continue
			}
			tfVal, ok := parseLiteralAttr(tfRaw)
			if !ok {
				continue
			}
			if liveVal != tfVal {
				drifts = append(drifts, AttrDrift{Resource: r, Key: key, LiveVal: liveVal, TFVal: tfVal})
			}
		}
	}
	return drifts
}

// Converts LiveResource representing a VPC into a Terraform HCL block String
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
