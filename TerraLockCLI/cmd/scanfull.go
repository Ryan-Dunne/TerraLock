package cmd

import (
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

var scanfullCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan AWS for drift",
	Long:  "Scans AWS EC2 instances and outputs a pretty‑printed JSON report.",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("scanfull called")

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

		// Print to console
		fmt.Println(string(pretty))

		// Auto-generate output filename
		filename := fmt.Sprintf("scan-output-%d.json", time.Now().Unix())

		// Write file
		err = os.WriteFile(filename, pretty, 0644)
		if err != nil {
			log.Fatal(err)
		}

		fmt.Printf("Output written to %s\n", filename)

		//	return ghCmd()
	},
}

func init() {
	rootCmd.AddCommand(scanCmd)
}
