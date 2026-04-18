package mapper

import (
	"fmt"
	"os"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
)

type TerraformResource struct {
	Type       string
	Name       string
	Attributes map[string]string
}

func ParseTerraform(path string) ([]TerraformResource, error) {

	parser := hclparse.NewParser()           // Keeps track of all parsed files contents
	file, diags := parser.ParseHCLFile(path) // Parses the HCL file(s) retrived from github

	if diags.HasErrors() {
		return nil, fmt.Errorf("%s", diags.Error()) //Checks for any severe errors during parsing and returns them if found
	}

	data, err := os.ReadFile(path) // Reads file content into a byte slice to get raw attribute values
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	schema := &hcl.BodySchema{ // Defines schema to only looka for "resource" blocks with 2 keywords, "type" & "name"
		Blocks: []hcl.BlockHeaderSchema{
			{
				Type:       "resource",
				LabelNames: []string{"type", "name"},
			},
		},
	}

	content, _, diags := file.Body.PartialContent(schema) // Use schema to extract only desired blocks from file content
	if diags.HasErrors() {
		return nil, fmt.Errorf("%s", diags.Error())
	}

	fmt.Println("Found blocks:", len(content.Blocks))

	var TerraformResources []TerraformResource

	for _, block := range content.Blocks { // Runs over each block, extracts attributes, adds to slice

		attributes := GetAttributes(block.Body, data)
		TerraformResources = append(TerraformResources, TerraformResource{
			Type:       block.Labels[0],
			Name:       block.Labels[1],
			Attributes: attributes,
		})
	}
	return TerraformResources, nil
}

func GetAttributes(body hcl.Body, fileBytes []byte) map[string]string {

	attrs, _ := body.JustAttributes() // Gets all attributes in the block, doesnt support nested blocks yet, discards error they may cause

	out := map[string]string{} // Create map to store attribute names and their raw values as strings

	for name, attr := range attrs { // Loop over each attribute
		r := attr.Expr.Range()                            //Get range in file
		raw := string(fileBytes[r.Start.Byte:r.End.Byte]) //Extract raw value as string using range
		out[name] = strings.TrimSpace(raw)
	}

	return out
}
