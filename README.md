# TerraLock

Terraform drift detection tool for my final year project.

## Setup On A New Machine (Windows PowerShell)

1. Clone the repository and move into the CLI folder:

```powershell
git clone https://github.com/Ryan-Dunne/TerraLock.git
cd TerraLock\TerraLockCLI
```

2. Restore dependencies and build the executable:

```powershell
go mod tidy
go build
```

3. Set AWS credentials for the current PowerShell session:

```powershell
$env:AWS_ACCESS_KEY_ID = "YOUR_KEY_HERE"
$env:AWS_SECRET_ACCESS_KEY = "YOUR_KEY_HERE"
$env:AWS_REGION = "YOUR_REGION_HERE"
```

4. Ensure you are logged into your GitHub CLI
```powershell
gh auth login
gh auth status
```


5. Verify the install:

```powershell
.\terralock.exe help
.\terralock.exe help scan
```

6. Run your first scan:

```powershell
.\terralock.exe scan
```

## Setup On A New Machine (Linux Bash)

```bash
git clone https://github.com/Ryan-Dunne/TerraLock.git
cd TerraLock/TerraLockCLI
go mod tidy
go build
export AWS_ACCESS_KEY_ID="YOUR_KEY_HERE"
export AWS_SECRET_ACCESS_KEY="YOUR_KEY_HERE"
export AWS_REGION="YOUR_REGION_HERE"
./terralock help
./terralock help scan
./terralock scan
```

Ensure you are logged into GitHub CLI

```bash
gh auth login
gh auth status
```

## Prerequisites

### Required installations

Go 1.25.x or later  
https://go.dev/doc/install

AWS CLI 2.31.x or later  
https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html

Terraform 1.14.x or later  
https://developer.hashicorp.com/terraform/install

GitHub CLI
https://cli.github.com/

## Authentication

Use AWS access keys with at least read-only permissions.

PowerShell environment variables:

```powershell
$env:AWS_ACCESS_KEY_ID = "YOUR_KEY_HERE"
$env:AWS_SECRET_ACCESS_KEY = "YOUR_KEY_HERE"
$env:AWS_REGION = "YOUR_REGION_HERE"
```

For commands that use GitHub data, authenticate with GitHub CLI.

## Command Purpose

| Command | Purpose | Requires GitHub CLI | Primary output |
| --- | --- | --- | --- |
| terralock | Show help and top-level command usage | No | Console help text |
| terralock scan --repo <owner/repo> [--file <path>] [--dir <path>] | Compare live AWS instances against Terraform fetched from GitHub | Yes | missing-from-tf-<timestamp>.tf |
| terralock help [command] | Show help for a command | No | Console help text |

## Command Workflows

### terralock

1. Loads the CLI root command.
2. Lists available subcommands and global help text.
3. Exits without creating files.

### terralock scan --repo <owner/repo> [--file <path>] [--dir <path>]

1. Validates GitHub arguments and resolves one or more Terraform files.
2. Fetches Terraform file content from GitHub and combines it.
3. Writes combined content to gh-output-<timestamp>.tf.
4. Loads AWS credentials and scans live EC2 instances.
5. Maps scan output into the internal instance structure.
6. Parses Terraform resources from the fetched file.
7. Compares live instances against parsed Terraform resources.
8. Writes missing resources to missing-from-tf-<timestamp>.tf.
9. Removes temporary files if cleanup is reached.

### terralock help [command]

1. Loads help text for the requested command.
2. Prints usage, flags, and examples.
3. Exits without creating files.

## Outputs

- missing-from-tf-<timestamp>.tf: Generated Terraform resources for instances detected as missing from the comparison baseline.
- gh-output-<timestamp>.tf: Temporary Terraform content fetched from GitHub during scanfull.
- scan-output-<timestamp>.json: Temporary AWS scan snapshot used during processing.

Notes:
- A successful scan or scanfull run should produce a missing-from-tf file.
- Temporary files may remain if a run fails before cleanup.

## Commands

### Root command

terralock

Flags:
- -h, --help

### scan

terralock scan --repo <owner/repo> [--file <path>] [--dir <path>]

Flags:
- -r, --repo <owner/repo> (required)
- -f, --file <path/in/repo> (optional, required if --dir is not set)
- --dir <path/to/terraform/dir> (optional, required if --file is not set)
- -h, --help

### help

terralock help [command]

## Examples

terralock scan --repo github-username/your-repo --file Terraform/main.tf

terralock scan --repo github-username/your-repo --dir Terraform

