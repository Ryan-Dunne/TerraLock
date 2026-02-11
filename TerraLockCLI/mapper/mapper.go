package mapper

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
)

type AwsInstance struct {
	Instance         string `json:"instance_id"`
	AMI              string `json:"ami"`
	Type             string `json:"type"`
	AvailabilityZone string `json:"availability_zone"`
}

func FindInstances(path string) ([]AwsInstance, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("Read File failed: %w", err)
	}

	var instances []AwsInstance //Populates the slice with instances
	if err := json.Unmarshal(data, &instances); err != nil {
		return nil, fmt.Errorf("Unmarshal Error %w", err)
	}
	return instances, nil
}

type TerraformResource struct {
	Type       string
	Name       string
	Attributes map[string]string
}

func ParseTerraform(path string) ([]TerraformResource, error) {

	parser := hclparse.NewParser()
	file, diags := parser.ParseHCLFile(path)

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	if diags.HasErrors() {
		return nil, fmt.Errorf("%s", diags.Error())
	}

	schema := &hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{
				Type:       "resource",
				LabelNames: []string{"type", "name"},
			},
		},
	}

	content, _, diags := file.Body.PartialContent(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("%s", diags.Error())
	}

	fmt.Println("Found blocks:", len(content.Blocks))

	var TerraformResources []TerraformResource

	for _, block := range content.Blocks {
		resourceType := block.Labels[0]
		resourceName := block.Labels[1]

		attributes := GetAttributes(block.Body, data)

		TerraformResources = append(TerraformResources, TerraformResource{
			Type:       resourceType,
			Name:       resourceName,
			Attributes: attributes,
		})
	}
	return TerraformResources, nil
}

func GetAttributes(body hcl.Body, fileBytes []byte) map[string]string {
	attrs, _ := body.JustAttributes()

	out := map[string]string{}

	for name, attr := range attrs {
		r := attr.Expr.Range()
		raw := string(fileBytes[r.Start.Byte:r.End.Byte])
		out[name] = strings.TrimSpace(raw)
	}

	return out
}
