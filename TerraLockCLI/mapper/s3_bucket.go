package mapper

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3BucketScanner struct{}

// Returns the Terraform resource type that this scanner is responsible for,"aws_s3_bucket" for here
func (s *S3BucketScanner) TerraformType() string { return "aws_s3_bucket" }

// Fetches live S3 buckets from AWS, returns slice of LiveResources with relevant attributes extracted from each bucket
func (s *S3BucketScanner) Fetch(ctx context.Context, cfg aws.Config) ([]LiveResource, error) {
	client := s3.NewFromConfig(cfg)

	output, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return nil, fmt.Errorf("ListBuckets failed: %w", err)
	}

	var resources []LiveResource
	for _, bucket := range output.Buckets {
		name := aws.ToString(bucket.Name)
		resources = append(resources, LiveResource{
			ID:   name,
			Name: name,
			Attrs: map[string]string{
				"bucket": name,
			},
		})
	}
	return resources, nil
}

// Returns a slice of LiveResources representing S3 buckets that exist in AWS but not in Terraform
func (s *S3BucketScanner) FindMissing(terraform []TerraformResource, live []LiveResource) []LiveResource {
	known := map[string]struct{}{}
	for _, resource := range terraform {
		if resource.Type != s.TerraformType() {
			continue
		}
		if name, ok := resource.Attributes["bucket"]; ok {
			known[strings.Trim(name, "\"")] = struct{}{}
		}
	}

	var missing []LiveResource
	for _, resource := range live {
		if _, ok := known[resource.Name]; !ok {
			missing = append(missing, resource)
		}
	}
	return missing

}

// Converts a LiveResource representing an S3 bucket into a Terraform HCL block string, using a provided label for the resource name

func (s *S3BucketScanner) ToHCL(r LiveResource, label string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("resource \"aws_s3_bucket\" \"%s\" {\n", label))
	b.WriteString(fmt.Sprintf("  bucket = \"%s\"\n", r.Attrs["bucket"]))
	b.WriteString("}\n")
	return b.String()
}
