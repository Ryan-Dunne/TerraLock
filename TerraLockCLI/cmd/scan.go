package cmd

import (
	"TerraLock/TerraLockCLI/mapper"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/spf13/cobra"
)

type InstanceInfo struct {
	InstanceID string `json:"instance_id"`
	Name       string `json:"name"`
	Ami        string `json:"ami"`
	Type       string `json:"type"`
	AZ         string `json:"availability_zone"`
}

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan AWS for drift",
	Long:  "Scans AWS EC2 instances and outputs a pretty‑printed JSON report.",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("scan called")

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

		// Pretty print JSON
		pretty, err := json.MarshalIndent(instances, "", "  ")
		if err != nil {
			log.Fatal(err)
		}

		fmt.Println("\n== Instances ==")
		fmt.Println("----------------")
		for _, instance := range instances {
			fmt.Printf("- id=%s name=%s ami=%s type=%s az=%s\n", instance.InstanceID, instance.Name, instance.Ami, instance.Type, instance.AZ)
		}

		// Auto-generate output filename
		filename := fmt.Sprintf("scan-output-%d.json", time.Now().Unix())

		// Write file
		err = os.WriteFile(filename, pretty, 0644)
		if err != nil {
			log.Fatal(err)
		}

		fmt.Printf("Output written to %s\n", filename)

		result, err := mapper.FindInstances(filename)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("\n== Mapper ==")
		fmt.Println("----------------")
		for _, inst := range result {
			fmt.Printf("- id=%s name=%s ami=%s type=%s az=%s\n", inst.Instance, inst.Name, inst.AMI, inst.Type, inst.AvailabilityZone)
		}

		// Find the most recent gh-output file
		ghOutputFiles, err := filepath.Glob("gh-output-*.tf")
		if err != nil || len(ghOutputFiles) == 0 {
			log.Fatal("No gh-output file found.")
		}
		ghOutputPath := ghOutputFiles[len(ghOutputFiles)-1]

		terraform, err := mapper.ParseTerraform(ghOutputPath)
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
			return
		}

		outPath := fmt.Sprintf("missing-from-tf-%d.tf", time.Now().Unix())
		if err := writeMissingInstances(outPath, missingInstances); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("\nMissing instances written to %s\n", outPath)

		// Clean up temporary files
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
	rootCmd.AddCommand(scanCmd)
}

func findMissingInstances(terraform []mapper.TerraformResource, live []mapper.AwsInstance) []mapper.AwsInstance {
	known := map[string]struct{}{}
	for _, resource := range terraform {
		if resource.Type != "aws_instance" {
			continue
		}
		name := extractTagName(resource.Attributes["tags"])
		if name == "" {
			continue
		}
		known[name] = struct{}{}
	}

	missingInstances := make([]mapper.AwsInstance, 0)
	for _, inst := range live {
		if inst.Name == "" {
			continue
		}
		if _, exists := known[inst.Name]; !exists {
			missingInstances = append(missingInstances, inst)
		}
	}

	return missingInstances
}

func extractTagName(tagsExpr string) string {
	if tagsExpr == "" {
		return ""
	}
	compact := strings.ReplaceAll(tagsExpr, "\n", " ")
	idx := strings.Index(compact, "Name")
	if idx == -1 {
		return ""
	}
	segment := compact[idx:]
	eq := strings.Index(segment, "=")
	if eq == -1 {
		return ""
	}
	segment = segment[eq+1:]
	firstQuote := strings.Index(segment, "\"")
	if firstQuote == -1 {
		return ""
	}
	segment = segment[firstQuote+1:]
	secondQuote := strings.Index(segment, "\"")
	if secondQuote == -1 {
		return ""
	}
	return segment[:secondQuote]
}

func writeMissingInstances(path string, instances []mapper.AwsInstance) error {
	var builder strings.Builder
	builder.WriteString("// Generated from live AWS instances\n")

	used := map[string]int{}
	for _, inst := range instances {
		label := sanitizeResourceName(inst.Name)
		if label == "" {
			label = sanitizeResourceName(inst.Instance)
		}
		used[label]++
		if used[label] > 1 {
			label = fmt.Sprintf("%s_%d", label, used[label])
		}

		builder.WriteString("\nresource \"aws_instance\" \"")
		builder.WriteString(label)
		builder.WriteString("\" {\n")
		builder.WriteString(fmt.Sprintf("  ami = \"%s\"\n", inst.AMI))
		builder.WriteString(fmt.Sprintf("  instance_type = \"%s\"\n", inst.Type))
		builder.WriteString(fmt.Sprintf("  availability_zone = \"%s\"\n", inst.AvailabilityZone))
		if inst.Name != "" {
			builder.WriteString("  tags = {\n")
			builder.WriteString(fmt.Sprintf("    Name = \"%s\"\n", inst.Name))
			builder.WriteString("  }\n")
		}
		builder.WriteString("}\n")
	}

	return os.WriteFile(path, []byte(builder.String()), 0644)
}

func sanitizeResourceName(name string) string {
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
