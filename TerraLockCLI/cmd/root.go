package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "terralock",
	Short: "Detect drift between your AWS infrastructure and Terraform",
	Long: `TerraLock scans your live AWS environment and compares it against
Terraform configuration fetched directly from GitHub, identifying resources
that exist in AWS but are missing from your Terraform state.

Example:
  terralock scan --repo owner/repo --file infra/main.tf
  terralock scan --repo owner/repo --dir infra/ --only ec2,s3
  terralock scan --repo owner/repo --file infra/main.tf --skip iam`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
}
