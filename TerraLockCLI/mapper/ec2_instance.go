package mapper

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

var tagPairRe = regexp.MustCompile(`"?([\w-]+)"?\s*=\s*"([^"]*)"`)

// parseAllTags extracts all key/value pairs from a raw Terraform tags block expression
func parseAllTags(tagsExpr string) map[string]string {
	tags := map[string]string{}
	for _, m := range tagPairRe.FindAllStringSubmatch(tagsExpr, -1) {
		tags[m[1]] = m[2]
	}
	return tags
}

type EC2InstanceScanner struct{}

// Returns the Terraform resource type that this scanner is responsible for,"aws_instance" for here
func (s *EC2InstanceScanner) TerraformType() string { return "aws_instance" }

// Fetches live EC2 instances from AWS, returns slice of LiveResources with relevant attributes extracted from each instamce
func (s *EC2InstanceScanner) Fetch(ctx context.Context, cfg aws.Config) ([]LiveResource, error) { 
	client := ec2.NewFromConfig(cfg)

	var resources []LiveResource
	paginator := ec2.NewDescribeInstancesPaginator(client, &ec2.DescribeInstancesInput{}) //If theres multiple pages, pagination will get them all
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("DescribeInstances failed: %w", err)
		}
		for _, reservation := range output.Reservations { //Iterate through each reservation, then through each instance within to get attributes & tags for LiveResource
			for _, instance := range reservation.Instances {
			name := ""
			tags := map[string]string{}
			for _, tag := range instance.Tags {
				k := aws.ToString(tag.Key)
				v := aws.ToString(tag.Value)
				tags[k] = v
				if k == "Name" {
					name = v
				}
			}

			attrs := map[string]string{ // Extracts relevant attributes from the EC2 instance to be used in the Terraform resource block
				"ami":               aws.ToString(instance.ImageId),
				"instance_type":     string(instance.InstanceType),
				"availability_zone": aws.ToString(instance.Placement.AvailabilityZone),
				"tenancy":           string(instance.Placement.Tenancy),
			}
			if kn := aws.ToString(instance.KeyName); kn != "" {
				attrs["key_name"] = kn
			}
			if id := aws.ToString(instance.SubnetId); id != "" {
				attrs["subnet_id"] = id
			}
			if instance.IamInstanceProfile != nil {
				attrs["iam_instance_profile"] = aws.ToString(instance.IamInstanceProfile.Arn)
			}

			resources = append(resources, LiveResource{
				ID:    aws.ToString(instance.InstanceId),
				Name:  name,
				Attrs: attrs,
				Tags:  tags,
			})
			}
		}
	}
	return resources, nil
}

// Compares live EC2 instances to Terraform resources, identifies any that exist in AWS but not in IaC based on "Name" tag
func (s *EC2InstanceScanner) FindMissing(terraform []TerraformResource, live []LiveResource) []LiveResource {
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
	for _, r := range live {
		if r.Name == "" {
			continue
		}
		if _, exists := known[r.Name]; !exists {
			missing = append(missing, r)
		}
	}
	return missing
}

// Compares live EC2 instances to Terraform resources by Name tag, returns tag value discrepancies
func (s *EC2InstanceScanner) FindTagDrift(terraform []TerraformResource, live []LiveResource) []TagDrift {
	return findTagDriftByNameTag(terraform, live, s.TerraformType())
}

// Compares live EC2 instances to Terraform resources, returns attribute value discrepancies for ami and instance_type
func (s *EC2InstanceScanner) FindAttrDrift(terraform []TerraformResource, live []LiveResource) []AttrDrift {
	comparable := []string{"ami", "instance_type", "key_name", "tenancy"}

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
		for _, key := range comparable {
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

// Converts a LiveResource representing an EC2 instance into a Terraform HCL block string, using a provided label for the resource name
func (s *EC2InstanceScanner) ToHCL(r LiveResource, label string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("\nresource \"aws_instance\" \"%s\" {\n", label))
	for _, key := range []string{"ami", "instance_type", "availability_zone", "subnet_id", "iam_instance_profile"} {
		if v, ok := r.Attrs[key]; ok {
			b.WriteString(fmt.Sprintf("  %s = \"%s\"\n", key, v))
		}
	}
	if r.Name != "" {
		b.WriteString("  tags = {\n")
		b.WriteString(fmt.Sprintf("    Name = \"%s\"\n", r.Name))
		b.WriteString("  }\n")
	}
	b.WriteString("}\n")
	return b.String()
}

// Helper function to extract the "Name" tag value from a raw tags expression in Terraform, returns empty string if not found
func extractTagName(tagsExpr string) string {
	if tagsExpr == "" {
		return ""
	}
	noNewLines := strings.ReplaceAll(tagsExpr, "\n", " ") // Remove newlines for easier parsing
	idx := strings.Index(noNewLines, "Name")              // Find "Name" key in tags expression
	if idx == -1 {
		return ""
	}
	segment := noNewLines[idx:] // Get substring starting from "Name" key to find its value
	eq := strings.Index(segment, "=")
	if eq == -1 {
		return ""
	}
	segment = segment[eq+1:] // Move past the equals sign to get to the value
	firstQuote := strings.Index(segment, "\"")
	if firstQuote == -1 {
		return ""
	}
	segment = segment[firstQuote+1:] // Move past the first quote to get to the start of the value
	secondQuote := strings.Index(segment, "\"")
	if secondQuote == -1 {
		return ""
	}
	return segment[:secondQuote]
}
