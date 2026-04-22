package mapper

import (
	"context"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
)

type Block struct { // Represents a nested block within a Terraform resource, such as "ingress" in a security group, with its own type and attributes
	Type  string
	Attrs map[string]string
}

type LiveResource struct { // Represents a resource that exists in the live AWS environment, with its ID, name, attributes, and any nested blocks
	ID     string
	Name   string
	Attrs  map[string]string
	Tags   map[string]string
	Blocks []Block
}

// TagDrift represents a single tag discrepancy between a live AWS resource and its Terraform definition
type TagDrift struct {
	Resource LiveResource
	Key      string
	LiveVal  string // empty if tag is missing from AWS
	TFVal    string // empty if tag is missing from Terraform
}

// AttrDrift represents a single attribute value discrepancy between a live AWS resource and its Terraform definition
type AttrDrift struct {
	Resource LiveResource
	Key      string
	LiveVal  string
	TFVal    string
}

// parseLiteralAttr strips quotes from a raw HCL string value; returns ("", false) for expressions like references
func parseLiteralAttr(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "\"") && strings.HasSuffix(raw, "\"") {
		return raw[1 : len(raw)-1], true
	}
	if strings.ContainsAny(raw, ".([$") {
		return "", false
	}
	return raw, true
}

// compareTags compares a live resource's tags against a Terraform tag map and returns any discrepancies
func compareTags(r LiveResource, tfTags map[string]string) []TagDrift {
	var drifts []TagDrift
	for key, liveVal := range r.Tags {
		if tfVal, exists := tfTags[key]; !exists {
			drifts = append(drifts, TagDrift{Resource: r, Key: key, LiveVal: liveVal, TFVal: ""})
		} else if tfVal != liveVal {
			drifts = append(drifts, TagDrift{Resource: r, Key: key, LiveVal: liveVal, TFVal: tfVal})
		}
	}
	for key, tfVal := range tfTags {
		if _, exists := r.Tags[key]; !exists {
			drifts = append(drifts, TagDrift{Resource: r, Key: key, LiveVal: "", TFVal: tfVal})
		}
	}
	return drifts
}

// findTagDriftByNameTag matches resorces by Name tag and compares all tag key/value pairs
func findTagDriftByNameTag(tfResources []TerraformResource, live []LiveResource, resourceType string) []TagDrift {
	tfTagsByName := map[string]map[string]string{}
	for _, r := range tfResources {
		if r.Type != resourceType {
			continue
		}
		name := extractTagName(r.Attributes["tags"])
		if name == "" {
			continue
		}
		tfTagsByName[name] = parseAllTags(r.Attributes["tags"])
	}

	var drifts []TagDrift // Iterate over live resources, find matching Terraform resource by Name tag, compare tags and collect drifts
	for _, r := range live {
		if r.Name == "" || r.Tags == nil {
			continue
		}
		tfTags, matched := tfTagsByName[r.Name]
		if !matched {
			continue
		}
		drifts = append(drifts, compareTags(r, tfTags)...)
	}
	sort.Slice(drifts, func(i, j int) bool {
		if drifts[i].Resource.Name != drifts[j].Resource.Name {
			return drifts[i].Resource.Name < drifts[j].Resource.Name
		}
		return drifts[i].Key < drifts[j].Key
	})
	return drifts
}

// Defines the interface all resource scanners must implement, including methods to fetch live resources, find missing ones compared to Terraform, and convert to HCL
type ResourceScanner interface {
	TerraformType() string
	Fetch(ctx context.Context, cfg aws.Config) ([]LiveResource, error)
	FindMissing(terraform []TerraformResource, live []LiveResource) []LiveResource
	FindTagDrift(terraform []TerraformResource, live []LiveResource) []TagDrift
	FindAttrDrift(terraform []TerraformResource, live []LiveResource) []AttrDrift
	ToHCL(resource LiveResource, label string) string
}

// Sanitizes a string to be used as a Terraform resource label by replacing invalid characters with underscores and converting to lowercase
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
