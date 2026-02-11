package cmd

import (
	"TerraLock/TerraLockCLI/mapper"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/spf13/cobra"
)

type InstanceInfo struct {
	InstanceID string `json:"instance_id"`
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
				instances = append(instances, InstanceInfo{
					InstanceID: aws.ToString(instance.InstanceId),
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
			fmt.Printf("- id=%s ami=%s type=%s az=%s\n", instance.InstanceID, instance.Ami, instance.Type, instance.AZ)
		}

		// Auto-generate output filename
		filename := fmt.Sprintf("scan-output-%d.json", time.Now().Unix())

		// Write file
		err = os.WriteFile(filename, pretty, 0644)
		if err != nil {
			log.Fatal(err)
		}

		fmt.Printf("Output written to %s\n", filename)

		result, err := mapper.FindInstances("C:\\Users\\RyanJ\\Desktop\\TerraLock\\TerraLockCLI\\scan-output-1769269407.json")
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("\n== Mapper ==")
		fmt.Println("----------------")
		for _, inst := range result {
			fmt.Printf("- id=%s ami=%s type=%s az=%s\n", inst.Instance, inst.AMI, inst.Type, inst.AvailabilityZone)
		}

		terraform, err := mapper.ParseTerraform("C:\\Users\\RyanJ\\Desktop\\TerraLock\\TerraLockCLI\\gh-output-1769271472.tf")
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

	},
}

func init() {
	rootCmd.AddCommand(scanCmd)
}
