package mapper

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

type SecurityGroupScanner struct{}

// TerraformType returns the Terraform resource type for security groups
func (s *SecurityGroupScanner) TerraformType() string { return "aws_security_group" }

// Fetches live security groups from AWS, returns slice of LiveResources with relevant attributes extracted from each group
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
			tags := map[string]string{}
			for _, tag := range sg.Tags {
				k := aws.ToString(tag.Key)
				v := aws.ToString(tag.Value)
				tags[k] = v
				if k == "Name" {
					name = v
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
				Tags:   tags,
				Blocks: blocks,
			})
		}
	}
	return resources, nil
}

// Converts an EC2 IpPermission to a Block representing either an ingress or egress rule in a security group, extracting protocol, ports, and CIDR blocks
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

// Returns a slice of LiveResources representing security groups that exist in AWS but not in Terraform, uses "name" attribute for comparison
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

// Compares live security groups to Terraform resources by Name tag, returns tag value discrepancies
func (s *SecurityGroupScanner) FindTagDrift(terraform []TerraformResource, live []LiveResource) []TagDrift {
	return findTagDriftByNameTag(terraform, live, s.TerraformType())
}

// normalizeCIDRList normalises both TF list literals ["a","b"] and live comma-separated "a,b" to a sorted string
func normalizeCIDRList(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var cidrs []string
	if strings.HasPrefix(raw, "[") {
		raw = strings.Trim(raw, "[]")
		for _, part := range strings.Split(raw, ",") {
			cidr := strings.Trim(strings.TrimSpace(part), "\"")
			if cidr != "" {
				cidrs = append(cidrs, cidr)
			}
		}
	} else {
		for _, part := range strings.Split(raw, ",") {
			if cidr := strings.TrimSpace(part); cidr != "" {
				cidrs = append(cidrs, cidr)
			}
		}
	}
	sort.Strings(cidrs)
	return strings.Join(cidrs, ",")
}

// normalizeRuleAttr strips surrounding quotes from a raw HCL scalar value
func normalizeRuleAttr(raw string) string {
	if v, ok := parseLiteralAttr(raw); ok {
		return v
	}
	return raw
}

// canonicalRule produces a normalised string representation of an ingress/egress rule for set comparison
func canonicalRule(blockType string, attrs map[string]string) string {
	return fmt.Sprintf("%s:%s:%s-%s:ipv4=%s:ipv6=%s",
		blockType,
		normalizeRuleAttr(attrs["protocol"]),
		normalizeRuleAttr(attrs["from_port"]),
		normalizeRuleAttr(attrs["to_port"]),
		normalizeCIDRList(attrs["cidr_blocks"]),
		normalizeCIDRList(attrs["ipv6_cidr_blocks"]),
	)
}

// Compares live security groups to Terraform resources, returns discrepancies for description and ingress/egress rules
func (s *SecurityGroupScanner) FindAttrDrift(terraform []TerraformResource, live []LiveResource) []AttrDrift {
	comparable := []string{"description"}

	type tfRecord struct {
		attrs  map[string]string
		blocks []Block
	}
	tfByName := map[string]tfRecord{}
	for _, r := range terraform {
		if r.Type != s.TerraformType() {
			continue
		}
		if name, ok := r.Attributes["name"]; ok {
			tfByName[strings.Trim(name, "\"")] = tfRecord{r.Attributes, r.Blocks}
		}
	}

	var drifts []AttrDrift
	for _, r := range live {
		rec, ok := tfByName[r.Attrs["name"]]
		if !ok {
			continue
		}

		// Compare flat attributes
		for _, key := range comparable {
			liveVal, hasLive := r.Attrs[key]
			tfRaw, hasTF := rec.attrs[key]
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

		// Compare ingress/egress rules as sets
		liveRules := map[string]struct{}{}
		for _, b := range r.Blocks {
			liveRules[canonicalRule(b.Type, b.Attrs)] = struct{}{}
		}
		tfRules := map[string]struct{}{}
		for _, b := range rec.blocks {
			tfRules[canonicalRule(b.Type, b.Attrs)] = struct{}{}
		}
		for rule := range liveRules {
			if _, exists := tfRules[rule]; !exists {
				drifts = append(drifts, AttrDrift{Resource: r, Key: "rule", LiveVal: rule, TFVal: ""})
			}
		}
		for rule := range tfRules {
			if _, exists := liveRules[rule]; !exists {
				drifts = append(drifts, AttrDrift{Resource: r, Key: "rule", LiveVal: "", TFVal: rule})
			}
		}
	}
	return drifts
}

// Converts LiveResource representing a security group into a Terraform HCL block String
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
