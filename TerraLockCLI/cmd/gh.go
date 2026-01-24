package cmd

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/spf13/cobra"
)

var filePath string

// ghCmd represents the gh command
var ghCmd = &cobra.Command{
	Use:   "gh <owner/repo>",
	Short: "Read a file from a GitHub repository",
	Long: `Reads a file from a GitHub repository using the GitHub CLI (gh)
without cloning the repository.`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 1 {
			log.Fatal("Usage: TerraLockCLI gh <owner/repo> --file <path>")
		}

		if filePath == "" {
			log.Fatal("You must specify --file <path/to/file>")
		}

		repo := args[0]

		// Call GitHub API via gh
		apiCmd := exec.Command("gh", "api",
			fmt.Sprintf("repos/%s/contents/%s", repo, filePath),
		)

		output, err := apiCmd.Output()
		if err != nil {
			log.Fatalf("gh api failed: %v", err)
		}

		// Parse JSON response
		var resp struct {
			Content  string `json:"content"`
			Encoding string `json:"encoding"`
		}

		if err := json.Unmarshal(output, &resp); err != nil {
			log.Fatalf("failed to parse JSON: %v", err)
		}

		// Decode Base64 content
		decoded, err := base64.StdEncoding.DecodeString(resp.Content)
		if err != nil {
			log.Fatalf("failed to decode file content: %v", err)
		}

		fmt.Println(string(decoded))

		var pretty []byte
		var jsonObj interface{}

		if json.Unmarshal(decoded, &jsonObj) == nil {
			pretty, err = json.MarshalIndent(jsonObj, "", "  ")
			if err != nil {
				log.Fatalf("failed to pretty print JSON: %v", err)
			}
		} else {
			// Not JSON → store raw text as-is
			pretty = decoded
		}

		// Auto-generate output filename
		filename := fmt.Sprintf("gh-output-%d.tf", time.Now().Unix())

		// Write file
		err = os.WriteFile(filename, pretty, 0644)
		if err != nil {
			log.Fatalf("failed to write output: %v", err)
		}

		fmt.Printf("Output written to %s\n", filename)

	},
}

func init() {
	ghCmd.Flags().StringVarP(&filePath, "file", "f", "", "Path to file inside the repo")
	rootCmd.AddCommand(ghCmd)
}
