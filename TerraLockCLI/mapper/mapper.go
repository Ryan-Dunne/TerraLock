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
	Blocks     []Block
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

	for _, block := range content.Blocks { // Iterate over each resource block found in file, extract attributes & nested blocks, append to TerraformResources slice
		attributes, tfBlocks := GetAttributesAndBlocks(block.Body, data)
		TerraformResources = append(TerraformResources, TerraformResource{
			Type:       block.Labels[0],
			Name:       block.Labels[1],
			Attributes: attributes,
			Blocks:     tfBlocks,
		})
	}
	return TerraformResources, nil
}

// GetAttributesAndBlocks extracts both flat attributes and nested ingress/egress blocks from a resource body
func GetAttributesAndBlocks(body hcl.Body, fileBytes []byte) (map[string]string, []Block) {
	nestedSchema := &hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "ingress"},
			{Type: "egress"},
		},
	}
	blockContent, remain, _ := body.PartialContent(nestedSchema) // Extract toplevel attributes first, look for any nested blocks defined in schema, extract their attributes as well

	attrs, _ := remain.JustAttributes()
	out := map[string]string{} // Extract flat attributes from the resource body, get raw HCL values using the file byte slice and store in output map
	for name, attr := range attrs {
		r := attr.Expr.Range()
		raw := string(fileBytes[r.Start.Byte:r.End.Byte])
		out[name] = strings.TrimSpace(raw)
	}

	var blocks []Block
	for _, b := range blockContent.Blocks { // Iterate over any nested blocks found, extract their attributes into a Block struct with type & attribute map
		bAttrs, _ := b.Body.JustAttributes()
		bAttrsMap := map[string]string{}
		for name, attr := range bAttrs {
			r := attr.Expr.Range()
			raw := string(fileBytes[r.Start.Byte:r.End.Byte])
			bAttrsMap[name] = strings.TrimSpace(raw)
		}
		blocks = append(blocks, Block{Type: b.Type, Attrs: bAttrsMap})
	}

	return out, blocks
}

// Returns a slice of LiveResources representing VPCs that exist in Terraform but not in AWS, uses "name" for comparison
func GetAttributes(body hcl.Body, fileBytes []byte) map[string]string {
	out, _ := GetAttributesAndBlocks(body, fileBytes)
	return out
}
