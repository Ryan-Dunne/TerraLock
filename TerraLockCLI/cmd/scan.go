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
)

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

		ghOutputFilename := fmt.Sprintf("gh-output-%d.tf", time.Now().Unix())
		err := os.WriteFile(ghOutputFilename, []byte(combinedTerraform.String()), 0644)
		if err != nil {
			log.Fatalf("failed to write output: %v", err)
		}

		defer os.Remove(ghOutputFilename)

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
		scanners := defaultScanners()
		var allResults []scannerResult
		for _, scanner := range scanners {
			live, err := scanner.Fetch(context.TODO(), cfg)
			if err != nil {
				log.Fatal(err)
			}

			missing := scanner.FindMissing(terraform, live) //Compares live resources to declared IaC
			fmt.Printf("  %s: %d live, %d missing\n", scanner.TerraformType(), len(live), len(missing))
			allResults = append(allResults, scannerResult{scanner, missing})
		}

		totalMissing := 0
		for _, res := range allResults {
			totalMissing += len(res.missing)
		}

		if totalMissing == 0 {
			fmt.Println("\nNo missing resources found.")
			return
		}

		// 4: Write missing resources to new Terraform file
		outPath := fmt.Sprintf("missing-from-tf-%d.tf", time.Now().Unix())
		if err := writeMissingResources(outPath, allResults); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("\nMissing resources written to %s\n", outPath)

	},
}

func init() {
	scanCmd.Flags().StringVarP(&ghRepo, "repo", "r", "", "GitHub repository (owner/repo)")
	scanCmd.Flags().StringVarP(&ghFilePath, "file", "f", "", "Path to file inside the repo")
	scanCmd.Flags().StringVar(&ghDir, "dir", "", "Path to Terraform directory in Github")
	rootCmd.AddCommand(scanCmd)
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

type scannerResult struct {
	scanner mapper.ResourceScanner
	missing []mapper.LiveResource
}

// Each Resource will have a corresponding scanner that knows how to fetch relative resource
func defaultScanners() []mapper.ResourceScanner {
	return []mapper.ResourceScanner{
		&mapper.EC2InstanceScanner{},
		&mapper.SecurityGroupScanner{},
		&mapper.IAMRoleScanner{},
		&mapper.S3BucketScanner{},
		// More scanners to go here
	}
}

// Writes missing resources to a new Terraform file, ensuring unique labels if needed
func writeMissingResources(path string, results []scannerResult) error {
	var builder strings.Builder
	builder.WriteString("// Generated by TerraLock\n")

	used := map[string]int{}
	for _, r := range results {
		for _, resource := range r.missing {
			label := mapper.SanitizeResourceName(resource.Name)
			if label == "" {
				label = mapper.SanitizeResourceName(resource.ID)
			}
			used[label]++
			if used[label] > 1 {
				label = fmt.Sprintf("%s_%d", label, used[label])
			}
			builder.WriteString(r.scanner.ToHCL(resource, label))
		}
	}

	return os.WriteFile(path, []byte(builder.String()), 0644)
}
