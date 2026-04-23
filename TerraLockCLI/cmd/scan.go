package cmd

import (
	"TerraLock/terralock/mapper"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/spf13/cobra"
)

var (
	ghRepo     string
	ghFilePath string //Flag variables
	ghDir      string
	scanOnly   []string
	scanSkip   []string
)

// Used to allow users to use short hand names for scanners in command flags
var scannerAliases = map[string]string{
	"ec2": "aws_instance",
	"s3":  "aws_s3_bucket",
	"sg":  "aws_security_group",
	"iam": "aws_iam_role",
	"vpc": "aws_vpc",
}

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Fetch Terraform from GitHub and scan AWS for drift",
	Long:  "Fetches a Terraform file from GitHub, scans AWS EC2 instances, and generates missing resources.",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("scan called")

		//1: Get Terraform file
		if ghRepo == "" {
			log.Fatal("You must specify --repo <owner/repo>")
		}

		if ghFilePath == "" && ghDir == "" {
			log.Fatal("You must specify either --file <path/to/file> or --tf-dir <path/to/terraform/dir>")
		}

		filePaths := make([]string, 0) //Collect file paths to fetch, does single file & directorys
		if ghFilePath != "" {
			filePaths = append(filePaths, ghFilePath)
		}
		if ghDir != "" {
			dirFiles, err := listTerraformFilesInDir(ghRepo, ghDir)
			if err != nil {
				log.Fatal(err)
			}
			filePaths = append(filePaths, dirFiles...)
		}

		if len(filePaths) == 0 {
			log.Fatal("No Terraform files found")
		}

		// Remove duplicates and sort file paths for consistent output
		seen := map[string]struct{}{}
		uniquePaths := make([]string, 0, len(filePaths))
		for _, p := range filePaths {
			if _, exists := seen[p]; exists {
				continue
			}
			seen[p] = struct{}{}
			uniquePaths = append(uniquePaths, p)
		}
		sort.Strings(uniquePaths)

		fmt.Printf("\n== Fetching %d Terraform file(s) from GitHub repo %s ==\n", len(uniquePaths), ghRepo)

		// 2: Fetch each file, combine contents, and parse
		var combinedTerraform strings.Builder
		for _, path := range uniquePaths {
			fmt.Printf("- %s\n", path)
			decoded, err := fetchGitHubDirectory(ghRepo, path)
			if err != nil {
				log.Fatalf("failed to fetch %s: %v", path, err)
			}
			combinedTerraform.WriteString("\n")
			combinedTerraform.WriteString("# source: ")
			combinedTerraform.WriteString(path)
			combinedTerraform.WriteString("\n")
			combinedTerraform.Write(decoded)
			combinedTerraform.WriteString("\n")
		}

		ghOutputFilename := fmt.Sprintf("gh-output-%d.tf", time.Now().Unix()) //Write combined Terraform to temp file for parsing
		err := os.WriteFile(ghOutputFilename, []byte(combinedTerraform.String()), 0644)
		if err != nil {
			log.Fatalf("failed to write output: %v", err)
		}

		defer os.Remove(ghOutputFilename) //Clean up temp file after execution

		terraform, err := mapper.ParseTerraform(ghOutputFilename)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("\n== Parsed %d Terraform resources ==\n", len(terraform))

		// 3: Scan AWS and compare to Terraform
		fmt.Println("\n== Scanning AWS ==")
		cfg, err := config.LoadDefaultConfig(context.TODO())
		if err != nil {
			log.Fatal(err)
		}

		// For each scanner type, fetch live resources, find missing ones, and collect results
		scanners, err := filterScanners(defaultScanners(), scanOnly, scanSkip)
		if err != nil {
			log.Fatal(err)
		}
		var allResults []scannerResult
		for _, scanner := range scanners { // Iterate through each scanner (EC2, S3, etc.)
			live, err := scanner.Fetch(context.TODO(), cfg)
			if err != nil {
				log.Fatal(err)
			}

			// For each scanner, find missing resources & drift, print summary, and collect results for file generation
			missing := scanner.FindMissing(terraform, live)
			tagDrifts := scanner.FindTagDrift(terraform, live)
			attrDrifts := scanner.FindAttrDrift(terraform, live)
			fmt.Printf("  %s: %d live, %d missing, %d tag drift(s), %d attr drift(s)\n", scanner.TerraformType(), len(live), len(missing), len(tagDrifts), len(attrDrifts))
			allResults = append(allResults, scannerResult{scanner, missing, tagDrifts, attrDrifts})
		}

		totalMissing := 0
		totalTagDrift := 0 // Calculate total counts across all scanners for summary output
		totalAttrDrift := 0
		for _, res := range allResults {
			totalMissing += len(res.missing)
			totalTagDrift += len(res.tagDrifts)
			totalAttrDrift += len(res.attrDrifts)
		}

		if totalMissing == 0 && totalTagDrift == 0 && totalAttrDrift == 0 {
			fmt.Println("\nNo issues found.")
			return
		}

		// Print tag drift report
		if totalTagDrift > 0 {
			fmt.Println("\n== Tag Drift ==")
			for _, res := range allResults {
				for _, d := range res.tagDrifts {
					switch {
					case d.TFVal == "":
						fmt.Printf("  [%s] %s: tag %q = %q in AWS (not in Terraform)\n", res.scanner.TerraformType(), d.Resource.Name, d.Key, d.LiveVal)
					case d.LiveVal == "":
						fmt.Printf("  [%s] %s: tag %q = %q in Terraform (missing in AWS)\n", res.scanner.TerraformType(), d.Resource.Name, d.Key, d.TFVal)
					default:
						fmt.Printf("  [%s] %s: tag %q = %q (AWS) vs %q (Terraform)\n", res.scanner.TerraformType(), d.Resource.Name, d.Key, d.LiveVal, d.TFVal)
					}
				}
			}
		}

		// Print attribute drift report
		if totalAttrDrift > 0 {
			fmt.Println("\n== Attribute Drift ==")
			for _, res := range allResults {
				for _, d := range res.attrDrifts {
					switch {
					case d.TFVal == "":
						fmt.Printf("  [%s] %s: %s %q in AWS (not in Terraform)\n", res.scanner.TerraformType(), d.Resource.Name, d.Key, d.LiveVal)
					case d.LiveVal == "":
						fmt.Printf("  [%s] %s: %s %q in Terraform (not in AWS)\n", res.scanner.TerraformType(), d.Resource.Name, d.Key, d.TFVal)
					default:
						fmt.Printf("  [%s] %s: %s = %q (AWS) vs %q (Terraform)\n", res.scanner.TerraformType(), d.Resource.Name, d.Key, d.LiveVal, d.TFVal)
					}
				}
			}
		}

		// 4: Write missing and drifted resources to Terraform file
		outPath := fmt.Sprintf("missing-from-tf-%d.tf", time.Now().Unix())
		if err := writeMissingResources(outPath, allResults); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("\nResources written to %s\n", outPath)

	},
}

func init() {
	scanCmd.Flags().StringVarP(&ghRepo, "repo", "r", "", "GitHub repository (owner/repo)")
	scanCmd.Flags().StringVarP(&ghFilePath, "file", "f", "", "Path to file inside the repo")
	scanCmd.Flags().StringVarP(&ghDir, "dir", "d", "", "Path to Terraform directory in Github")
	scanCmd.Flags().StringSliceVar(&scanOnly, "only", nil, "Comma-separated scanners to run (ec2,s3,sg,iam,vpc)")
	scanCmd.Flags().StringSliceVar(&scanSkip, "skip", nil, "Comma-separated scanners to skip (ec2,s3,sg,iam,vpc)")
	rootCmd.AddCommand(scanCmd)
}

// Filters the list of scanners based on --only and --skip flags, ensures they are mutually exclusive
func filterScanners(all []mapper.ResourceScanner, only, skip []string) ([]mapper.ResourceScanner, error) {
	if len(only) > 0 && len(skip) > 0 {
		return nil, fmt.Errorf("--only and --skip are mutually exclusive")
	}

	resolve := func(aliases []string) (map[string]bool, error) {
		types := make(map[string]bool, len(aliases))
		for _, a := range aliases {
			tfType, ok := scannerAliases[strings.ToLower(a)]
			if !ok {
				return nil, fmt.Errorf("unknown scanner %q — valid options: ec2, s3, sg, iam, vpc", a)
			}
			types[tfType] = true
		}
		return types, nil
	}

	if len(only) > 0 { // If --only is specified, include only those scanners
		types, err := resolve(only)
		if err != nil {
			return nil, err
		}
		var filtered []mapper.ResourceScanner
		for _, s := range all {
			if types[s.TerraformType()] {
				filtered = append(filtered, s)
			}
		}
		return filtered, nil
	}

	if len(skip) > 0 { // If --skip is specified, exclude those scanners
		types, err := resolve(skip)
		if err != nil {
			return nil, err
		}
		var filtered []mapper.ResourceScanner
		for _, s := range all {
			if !types[s.TerraformType()] {
				filtered = append(filtered, s)
			}
		}
		return filtered, nil
	}

	return all, nil
}

// Helper functions for GitHub API, resource comparison, and output
func fetchGitHubDirectory(repo, path string) ([]byte, error) {
	apiCmd := exec.Command("gh", "api", fmt.Sprintf("repos/%s/contents/%s", repo, path))
	ghOutput, err := apiCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh api failed for %s: %w", path, err)
	}

	var resp struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}

	if err := json.Unmarshal(ghOutput, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse JSON for %s: %w", path, err)
	}

	if resp.Encoding != "base64" {
		return nil, fmt.Errorf("unsupported encoding %q for %s", resp.Encoding, path)
	}

	decoded, err := base64.StdEncoding.DecodeString(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("failed to decode file content for %s: %w", path, err)
	}

	return decoded, nil
}

// Lists .tf files in a GitHub directory using the API
func listTerraformFilesInDir(repo, dir string) ([]string, error) {
	apiCmd := exec.Command("gh", "api", fmt.Sprintf("repos/%s/contents/%s", repo, dir))
	output, err := apiCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh api failed for directory %s: %w", dir, err)
	}

	var entries []struct {
		Type string `json:"type"`
		Path string `json:"path"`
		Name string `json:"name"`
	}

	if err := json.Unmarshal(output, &entries); err != nil {
		return nil, fmt.Errorf("failed to parse directory response for %s: %w", dir, err)
	}

	files := make([]string, 0)
	for _, entry := range entries {
		if entry.Type == "file" && strings.HasSuffix(entry.Name, ".tf") {
			files = append(files, entry.Path)
		}
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no .tf files found in directory %s", dir)
	}

	return files, nil
}

// Struct to hold classification of scan results for each scan type, used for file generation & output
type scannerResult struct {
	scanner    mapper.ResourceScanner
	missing    []mapper.LiveResource
	tagDrifts  []mapper.TagDrift
	attrDrifts []mapper.AttrDrift
}

// Each Resource will have a corresponding scanner that knows how to fetch relative resource
func defaultScanners() []mapper.ResourceScanner {
	return []mapper.ResourceScanner{
		&mapper.EC2InstanceScanner{},
		&mapper.SecurityGroupScanner{},
		&mapper.IAMRoleScanner{},
		&mapper.S3BucketScanner{},
		&mapper.VPCScanner{},
		// More scanners to go here
	}
}

// Writes missing and drifted resources to a Terraform file, ensuring unique labels if needed
func writeMissingResources(path string, results []scannerResult) error {
	var builder strings.Builder
	builder.WriteString("// Generated by TerraLock\n")

	used := map[string]int{} // Track used labels to ensure uniqueness across all resources when generating HCL blocks
	makeLabel := func(resource mapper.LiveResource) string {
		label := mapper.SanitizeResourceName(resource.Name)
		if label == "" {
			label = mapper.SanitizeResourceName(resource.ID)
		}
		used[label]++
		if used[label] > 1 { // If label has already been used, append a number to make it unique
			label = fmt.Sprintf("%s_%d", label, used[label])
		}
		return label
	}

	hasMissing := false
	for _, r := range results {
		if len(r.missing) > 0 {
			hasMissing = true
			break
		}
	}
	if hasMissing { // Only write missing resources section if there are any missing resources
		builder.WriteString("\n// == Missing from Terraform ==\n")
		for _, r := range results {
			for _, resource := range r.missing {
				builder.WriteString(r.scanner.ToHCL(resource, makeLabel(resource)))
			}
		}
	}

	// Collect unique drifted resources across tag and attr drift, avoid duplicates by resource ID
	seen := map[string]struct{}{}
	hasDrift := false
	for _, r := range results {
		for _, d := range r.tagDrifts {
			if _, ok := seen[d.Resource.ID]; !ok {
				seen[d.Resource.ID] = struct{}{}
				hasDrift = true
			}
		}
		for _, d := range r.attrDrifts {
			if _, ok := seen[d.Resource.ID]; !ok {
				seen[d.Resource.ID] = struct{}{}
				hasDrift = true
			}
		}
	}

	if hasDrift { // Only write drifted resources section if theres any drift to report
		builder.WriteString("\n// == Drifted Resources (corrected to live AWS values) ==\n")
		written := map[string]struct{}{}
		for _, r := range results {
			drifted := map[string]mapper.LiveResource{}
			for _, d := range r.tagDrifts {
				drifted[d.Resource.ID] = d.Resource
			}
			for _, d := range r.attrDrifts { // If a resource has both tag and attr drift, only written once because map keyed by resource ID
				drifted[d.Resource.ID] = d.Resource
			}
			ids := make([]string, 0, len(drifted))
			for id := range drifted {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			for _, id := range ids {
				if _, ok := written[id]; ok {
					continue
				}
				written[id] = struct{}{}
				builder.WriteString(r.scanner.ToHCL(drifted[id], makeLabel(drifted[id])))
			}
		}
	}

	return os.WriteFile(path, []byte(builder.String()), 0644)
}
