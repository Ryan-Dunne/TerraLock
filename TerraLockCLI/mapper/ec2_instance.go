package mapper

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

type EC2InstanceScanner struct{}

func (s *EC2InstanceScanner) TerraformType() string { return "aws_instance" }

func (s *EC2InstanceScanner) Fetch(ctx context.Context, cfg aws.Config) ([]LiveResource, error) {
	client := ec2.NewFromConfig(cfg)
	output, err := client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{})
	if err != nil {
		return nil, fmt.Errorf("DescribeInstances failed: %w", err)
	}

	// Loops through all instances in DescribeInstances output, extracts attributes, and constructs LiveResource objects for each
	var resources []LiveResource
	for _, reservation := range output.Reservations {
		for _, instance := range reservation.Instances {
			name := ""
			for _, tag := range instance.Tags {
				if aws.ToString(tag.Key) == "Name" {
					name = aws.ToString(tag.Value)
					break
				}
			}

			attrs := map[string]string{ // Extracts relevant attributes from the EC2 instance to be used in the Terraform resource block
				"ami":               aws.ToString(instance.ImageId),
				"instance_type":     string(instance.InstanceType),
				"availability_zone": aws.ToString(instance.Placement.AvailabilityZone),
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
			})
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
