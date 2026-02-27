package cmd

import (
	"TerraLock/TerraLockCLI/mapper"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/spf13/cobra"
)

var (
	ghRepo     string
	ghFilePath string
	ghDir      string
)

var scanfullCmd = &cobra.Command{
	Use:   "scanfull",
	Short: "Fetch Terraform from GitHub and scan AWS for drift",
	Long:  "Fetches a Terraform file from GitHub, scans AWS EC2 instances, and generates missing resources.",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("scanfull called")

		//1: Get Terraform file
		if ghRepo == "" {
			log.Fatal("You must specify --repo <owner/repo>")
		}

		if ghFilePath == "" && ghDir == "" {
			log.Fatal("You must specify either --file <path/to/file> or --tf-dir <path/to/terraform/dir>")
		}

		filePaths := make([]string, 0)
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
		fmt.Printf("GitHub file written to %s\n", ghOutputFilename)

		//2: Scan AWS instances
		fmt.Println("\n== Scanning AWS ==")
		cfg, err := config.LoadDefaultConfig(context.TODO())
		if err != nil {
			log.Fatal(err)
		}

		client := ec2.NewFromConfig(cfg)
		output, err := client.DescribeInstances(context.TODO(), &ec2.DescribeInstancesInput{})
		if err != nil {
			log.Fatal(err)
		}

		var instances []InstanceInfo
		for _, reservation := range output.Reservations {
			for _, instance := range reservation.Instances {
				nameTag := ""
				for _, tag := range instance.Tags {
					if aws.ToString(tag.Key) == "Name" {
						nameTag = aws.ToString(tag.Value)
						break
					}
				}
				instances = append(instances, InstanceInfo{
					InstanceID: aws.ToString(instance.InstanceId),
					Name:       nameTag,
					Ami:        aws.ToString(instance.ImageId),
					Type:       string(instance.InstanceType),
					AZ:         aws.ToString(instance.Placement.AvailabilityZone),
				})
			}
		}

		prettyJSON, err := json.MarshalIndent(instances, "", "  ")
		if err != nil {
			log.Fatal(err)
		}

		fmt.Println("\n== Instances ==")
		fmt.Println("----------------")
		for _, instance := range instances {
			fmt.Printf("- id=%s name=%s ami=%s type=%s az=%s\n", instance.InstanceID, instance.Name, instance.Ami, instance.Type, instance.AZ)
		}

		scanFilename := fmt.Sprintf("scan-output-%d.json", time.Now().Unix())
		err = os.WriteFile(scanFilename, prettyJSON, 0644)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Scan output written to %s\n", scanFilename)

		// Step 3: Parse and compare
		result, err := mapper.FindInstances(scanFilename)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("\n== Mapper ==")
		fmt.Println("----------------")
		for _, inst := range result {
			fmt.Printf("- id=%s name=%s ami=%s type=%s az=%s\n", inst.Instance, inst.Name, inst.AMI, inst.Type, inst.AvailabilityZone)
		}

		terraform, err := mapper.ParseTerraform(ghOutputFilename)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("\n== Terraform Resources ==")
		fmt.Println("-------------------------")
		for _, resource := range terraform {
			fmt.Printf("- %s.%s", resource.Type, resource.Name)
			for key, value := range resource.Attributes {
				fmt.Printf(" %s=%s", key, value)
			}
			fmt.Println()
		}

		missingInstances := findMissingInstances(terraform, result)
		if len(missingInstances) == 0 {
			fmt.Println("\nNo missing EC2 instances found.")

			// Clean up temporary files
			os.Remove(ghOutputFilename)
			os.Remove(scanFilename)
			return
		}

		outPath := fmt.Sprintf("missing-from-tf-%d.tf", time.Now().Unix())
		if err := writeMissingInstances(outPath, missingInstances); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("\nMissing instances written to %s\n", outPath)

		// Clean up temporary files
		ghOutputFiles, _ := filepath.Glob("gh-output-*.tf")
		for _, f := range ghOutputFiles {
			os.Remove(f)
		}
		scanOutputFiles, _ := filepath.Glob("scan-output-*.json")
		for _, f := range scanOutputFiles {
			os.Remove(f)
		}
	},
}

func init() {
	scanfullCmd.Flags().StringVarP(&ghRepo, "repo", "r", "", "GitHub repository (owner/repo)")
	scanfullCmd.Flags().StringVarP(&ghFilePath, "file", "f", "", "Path to file inside the repo")
	scanfullCmd.Flags().StringVar(&ghDir, "dir", "", "Path to Terraform directory in Github")
	rootCmd.AddCommand(scanfullCmd)
}

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
