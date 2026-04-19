package mapper

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

type SecurityGroupScanner struct{}

func (s *SecurityGroupScanner) TerraformType() string { return "aws_security_group" }

func (s *SecurityGroupScanner) Fetch(ctx context.Context, cfg aws.Config) ([]LiveResource, error) {
	client := ec2.NewFromConfig(cfg)

	var resources []LiveResource
	paginator := ec2.NewDescribeSecurityGroupsPaginator(client, &ec2.DescribeSecurityGroupsInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("DescribeSecurityGroups failed: %w", err)
		}
		for _, sg := range output.SecurityGroups {
			if aws.ToString(sg.GroupName) == "default" {
				continue
			}

			name := ""
			for _, tag := range sg.Tags {
				if aws.ToString(tag.Key) == "Name" {
					name = aws.ToString(tag.Value)
					break
				}
			}
			if name == "" {
				name = aws.ToString(sg.GroupId)
			}

			attrs := map[string]string{
				"name":        aws.ToString(sg.GroupName),
				"description": aws.ToString(sg.Description),
			}
			if vpcID := aws.ToString(sg.VpcId); vpcID != "" {
				attrs["vpc_id"] = vpcID
			}

			var blocks []Block
			for _, rule := range sg.IpPermissions {
				blocks = append(blocks, ipPermissionToBlock("ingress", rule))
			}
			for _, rule := range sg.IpPermissionsEgress {
				blocks = append(blocks, ipPermissionToBlock("egress", rule))
			}

			resources = append(resources, LiveResource{
				ID:     aws.ToString(sg.GroupId),
				Name:   name,
				Attrs:  attrs,
				Blocks: blocks,
			})
		}
	}
	return resources, nil
}

func ipPermissionToBlock(blockType string, perm ec2types.IpPermission) Block {
	fromPort := int32(0)
	toPort := int32(0)
	if perm.FromPort != nil {
		fromPort = *perm.FromPort
	}
	if perm.ToPort != nil {
		toPort = *perm.ToPort
	}

	attrs := map[string]string{
		"protocol":  aws.ToString(perm.IpProtocol),
		"from_port": fmt.Sprintf("%d", fromPort),
		"to_port":   fmt.Sprintf("%d", toPort),
	}

	if len(perm.IpRanges) > 0 {
		cidrs := make([]string, len(perm.IpRanges))
		for i, ipRange := range perm.IpRanges {
			cidrs[i] = aws.ToString(ipRange.CidrIp)
		}
		attrs["cidr_blocks"] = strings.Join(cidrs, ",")
	} else if len(perm.Ipv6Ranges) > 0 {
		cidrs := make([]string, len(perm.Ipv6Ranges))
		for i, cidr := range perm.Ipv6Ranges {
			cidrs[i] = aws.ToString(cidr.CidrIpv6)
		}
		attrs["ipv6_cidr_blocks"] = strings.Join(cidrs, ",")
	}

	return Block{
		Type:  blockType,
		Attrs: attrs,
	}
}

func (s *SecurityGroupScanner) FindMissing(terraform []TerraformResource, live []LiveResource) []LiveResource {
	known := map[string]struct{}{}
	for _, resource := range terraform {
		if resource.Type != s.TerraformType() {
			continue
		}
		if sgName, ok := resource.Attributes["name"]; ok {
			known[strings.Trim(sgName, "\"")] = struct{}{}
		}
	}

	var missing []LiveResource
	for _, r := range live {
		if _, exists := known[r.Attrs["name"]]; !exists {
			missing = append(missing, r)
		}
	}
	return missing
}

func (s *SecurityGroupScanner) ToHCL(r LiveResource, label string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("\nresource \"aws_security_group\" \"%s\" {\n", label))

	for _, key := range []string{"name", "description", "vpc_id"} {
		if v, ok := r.Attrs[key]; ok && v != "" {
			b.WriteString(fmt.Sprintf("  %s = \"%s\"\n", key, v))
		}
	}

	for _, block := range r.Blocks {
		b.WriteString(fmt.Sprintf("\n  %s {\n", block.Type))
		for _, key := range []string{"from_port", "to_port", "protocol", "cidr_blocks", "ipv6_cidr_blocks"} {
			if v, ok := block.Attrs[key]; ok {
				if key == "cidr_blocks" || key == "ipv6_cidr_blocks" {
					b.WriteString(fmt.Sprintf("    %s = [\"%s\"]\n", key, v))
				} else {
					b.WriteString(fmt.Sprintf("    %s = \"%s\"\n", key, v))
				}
			}
		}
		b.WriteString("  }\n")
	}

	if r.Name != "" {
		b.WriteString("\n  tags = {\n")
		b.WriteString(fmt.Sprintf("    Name = \"%s\"\n", r.Name))
		b.WriteString("  }\n")
	}

	b.WriteString("}\n")
	return b.String()
}
