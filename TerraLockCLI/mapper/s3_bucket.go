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

	output, err := client.ListBuckets(ctx, &s3.ListBucketsInput{}) // Only api call needed to list all buckets
	if err != nil {
		return nil, fmt.Errorf("ListBuckets failed: %w", err)
	}

	var resources []LiveResource
	for _, bucket := range output.Buckets { // For each bucket, get its name and tags
		name := aws.ToString(bucket.Name)

		tags := map[string]string{}
		tagsOut, err := client.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{Bucket: bucket.Name})
		if err == nil {
			for _, t := range tagsOut.TagSet {
				tags[aws.ToString(t.Key)] = aws.ToString(t.Value)
			}
		}

		resources = append(resources, LiveResource{
			ID:   name,
			Name: name,
			Attrs: map[string]string{
				"bucket": name,
			},
			Tags: tags,
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

// compares tags of live S3 buckets to their Terraform counterparts, returns slice TagDrift representing any differences found, uses bucket name for matching
func (s *S3BucketScanner) FindTagDrift(terraform []TerraformResource, live []LiveResource) []TagDrift {
	tfTagsByName := map[string]map[string]string{}
	for _, r := range terraform {
		if r.Type != s.TerraformType() {
			continue
		}
		if name, ok := r.Attributes["bucket"]; ok {
			tfTagsByName[strings.Trim(name, "\"")] = parseAllTags(r.Attributes["tags"])
		}
	}

	var drifts []TagDrift // For each live bucket, compare its tags to the corresponding Terraform resource's tags, matched by bucket name
	for _, r := range live {
		if r.Tags == nil {
			continue
		}
		tfTags, matched := tfTagsByName[r.Name]
		if !matched {
			continue
		}
		drifts = append(drifts, compareTags(r, tfTags)...)
	}
	return drifts
}

// S3 buckets have no literal attributes to compare beyond the bucket name (which is the matching key)
func (s *S3BucketScanner) FindAttrDrift(_ []TerraformResource, _ []LiveResource) []AttrDrift {
	return nil
}

// Converts a LiveResource representing an S3 bucket into a Terraform HCL block string, using a provided label for the resource name

func (s *S3BucketScanner) ToHCL(r LiveResource, label string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("resource \"aws_s3_bucket\" \"%s\" {\n", label))
	b.WriteString(fmt.Sprintf("  bucket = \"%s\"\n", r.Attrs["bucket"]))
	b.WriteString("}\n")
	return b.String()
}
