package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"dbtool/internal/interactivelist"
	"dbtool/internal/logger"
	"dbtool/internal/secureinput"
	"dbtool/internal/settings"

	"github.com/spf13/cobra"
)

// maskedPlaceholder is shown in prompts and displays in place of sensitive credential values.
const maskedPlaceholder = "****"

// restoreTempDirPrefix is the prefix used when creating temporary directories for S3 restore downloads.
const restoreTempDirPrefix = "dbtool-restore-*"

var s3Cmd = &cobra.Command{
	Use:   "s3",
	Short: "Manage S3 storage settings",
}

var s3ConfigStorageType string
var s3ConfigBucket string
var s3ConfigRegion string
var s3ConfigAccessKey string
var s3ConfigSecretKey string
var s3ConfigPrefix string
var s3ConfigEndpoint string

var s3ConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "Configure storage backend (local file or S3)",
	Long: `Configure where dump files are stored.

You can choose between local file storage (default) and S3-compatible
object storage. S3 settings are stored in ~/.dbtool/dbtool.settings.

Non-interactive example:
  dbtool s3 config --storage-type s3 --bucket my-bucket --region us-east-1 \
    --access-key AKID --secret-key secret
  dbtool s3 config --storage-type local`,
	Run: func(cmd *cobra.Command, args []string) {
		s := settings.Load()

		hasFlags := cmd.Flags().Changed("storage-type") ||
			cmd.Flags().Changed("bucket") ||
			cmd.Flags().Changed("region") ||
			cmd.Flags().Changed("access-key") ||
			cmd.Flags().Changed("secret-key") ||
			cmd.Flags().Changed("prefix") ||
			cmd.Flags().Changed("endpoint")

		if hasFlags {
			if !cmd.Flags().Changed("storage-type") {
				fmt.Println("--storage-type is required (local or s3).")
				os.Exit(1)
			}
			switch s3ConfigStorageType {
			case string(settings.StorageLocal):
				s.StorageType = settings.StorageLocal
			case string(settings.StorageS3):
				s.StorageType = settings.StorageS3
				if cmd.Flags().Changed("bucket") {
					s.S3.Bucket = s3ConfigBucket
				}
				if cmd.Flags().Changed("region") {
					s.S3.Region = s3ConfigRegion
				}
				if cmd.Flags().Changed("access-key") {
					s.S3.AccessKey = s3ConfigAccessKey
				}
				if cmd.Flags().Changed("secret-key") {
					s.S3.SecretKey = s3ConfigSecretKey
				}
				if cmd.Flags().Changed("prefix") {
					s.S3.Prefix = s3ConfigPrefix
				}
				if cmd.Flags().Changed("endpoint") {
					s.S3.Endpoint = s3ConfigEndpoint
				}
			default:
				fmt.Printf("Invalid --storage-type %q. Use \"local\" or \"s3\".\n", s3ConfigStorageType)
				os.Exit(1)
			}
		} else {
			reader := bufio.NewReader(os.Stdin)

			options := []string{"Local file storage", "S3 (or S3-compatible) object storage"}
			idx, err := interactivelist.SelectOne(fmt.Sprintf("Storage backend (current: %s)", s.StorageType), options)
			if err != nil {
				fmt.Printf("Selection canceled — keeping current storage type: %s\n", s.StorageType)
			} else {
				switch idx {
				case 0:
					s.StorageType = settings.StorageLocal
					fmt.Println("Storage set to: local")
				case 1:
					s.StorageType = settings.StorageS3
					s.S3 = askS3Config(reader, s.S3)
				}
			}
		}

		if err := settings.Save(s); err != nil {
			logger.Error("failed to save settings: %v", err)
			fmt.Println("Error saving settings:", err)
			os.Exit(1)
		}

		logger.Info("storage settings saved (type=%s)", s.StorageType)
		fmt.Println("Settings saved.")

		if s.StorageType == settings.StorageS3 {
			fmt.Printf("S3 bucket:   %s\n", s.S3.Bucket)
			fmt.Printf("S3 region:   %s\n", s.S3.Region)
			fmt.Printf("S3 prefix:   %s\n", s.S3.Prefix)
			if s.S3.Endpoint != "" {
				fmt.Printf("S3 endpoint: %s\n", s.S3.Endpoint)
			}
		}
	},
}

var s3ShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current storage configuration",
	Run: func(cmd *cobra.Command, args []string) {
		s := settings.Load()
		fmt.Printf("Storage type: %s\n", s.StorageType)
		if s.StorageType == settings.StorageS3 {
			fmt.Printf("Bucket:       %s\n", s.S3.Bucket)
			fmt.Printf("Region:       %s\n", s.S3.Region)
			fmt.Printf("Prefix:       %s\n", s.S3.Prefix)
			fmt.Printf("Endpoint:     %s\n", s.S3.Endpoint)
			if s.S3.AccessKey != "" {
				fmt.Printf("Access key:   %s\n", maskedPlaceholder)
			}
		}
	},
}

// awsRegions lists AWS's standard region codes, offered as a picker for
// the S3 Region field. S3-compatible providers (MinIO, etc.) don't use
// real AWS regions, so the list always ends with a Custom option that
// falls through to typing any value.
var awsRegions = []string{
	"us-east-1",
	"us-east-2",
	"us-west-1",
	"us-west-2",
	"af-south-1",
	"ap-east-1",
	"ap-south-1",
	"ap-south-2",
	"ap-southeast-1",
	"ap-southeast-2",
	"ap-southeast-3",
	"ap-southeast-4",
	"ap-northeast-1",
	"ap-northeast-2",
	"ap-northeast-3",
	"ca-central-1",
	"ca-west-1",
	"eu-central-1",
	"eu-central-2",
	"eu-west-1",
	"eu-west-2",
	"eu-west-3",
	"eu-north-1",
	"eu-south-1",
	"eu-south-2",
	"il-central-1",
	"me-central-1",
	"me-south-1",
	"sa-east-1",
}

const customRegionOption = "Custom (type a region — e.g. for MinIO or another S3-compatible provider)"

// askRegion offers awsRegions through the arrow-key picker, with current
// (if set) reordered to the front so it's preselected and Enter keeps it
// — even when current is a non-AWS value from a prior custom entry.
// Choosing the trailing Custom option falls through to a free-text prompt.
func askRegion(reader *bufio.Reader, current string) string {
	options := make([]string, 0, len(awsRegions)+2)
	if current != "" {
		options = append(options, current)
	}
	for _, r := range awsRegions {
		if r != current {
			options = append(options, r)
		}
	}
	options = append(options, customRegionOption)

	idx, err := interactivelist.SelectOne("S3 Region", options)
	if err != nil {
		return current
	}
	if options[idx] == customRegionOption {
		return interactivelist.Text(reader, "S3 Region (custom)", current)
	}
	return options[idx]
}

// askS3Config interactively prompts for S3 credentials, keeping existing
// values when the user presses Enter without input.
func askS3Config(reader *bufio.Reader, current settings.S3Config) settings.S3Config {
	// readSecret prompts for a sensitive value with masked (starred) input.
	// When a value is already set it shows a fixed placeholder so the actual
	// secret never flows into any print statement. Pressing Enter keeps the
	// current value; any non-empty input replaces it.
	readSecret := func(prompt, cur string) string {
		placeholder := ""
		if cur != "" {
			placeholder = maskedPlaceholder
		}
		val, err := secureinput.ReadPassword(interactivelist.PromptLabel(prompt, placeholder))
		if err != nil {
			fmt.Println("Error reading input:", err)
			return cur
		}
		val = strings.TrimSpace(val)
		if val == "" || val == maskedPlaceholder {
			// User pressed Enter or re-typed the placeholder — keep original.
			return cur
		}
		return val
	}

	cfg := current
	cfg.Bucket = interactivelist.Text(reader, "S3 Bucket", cfg.Bucket)
	cfg.Region = askRegion(reader, cfg.Region)
	cfg.AccessKey = readSecret("S3 Access Key", cfg.AccessKey)
	cfg.SecretKey = readSecret("S3 Secret Key", cfg.SecretKey)

	cfg.Prefix = interactivelist.Text(reader, "S3 Key Prefix (optional)", cfg.Prefix)
	cfg.Endpoint = interactivelist.Text(reader, "Custom Endpoint URL (optional, e.g. http://minio:9000)", cfg.Endpoint)

	return cfg
}

func init() {
	s3ConfigCmd.Flags().StringVar(&s3ConfigStorageType, "storage-type", "", "Storage type: local or s3 (non-interactive)")
	s3ConfigCmd.Flags().StringVar(&s3ConfigBucket, "bucket", "", "S3 bucket name (non-interactive)")
	s3ConfigCmd.Flags().StringVar(&s3ConfigRegion, "region", "", "S3 region (non-interactive)")
	s3ConfigCmd.Flags().StringVar(&s3ConfigAccessKey, "access-key", "", "S3 access key (non-interactive)")
	s3ConfigCmd.Flags().StringVar(&s3ConfigSecretKey, "secret-key", "", "S3 secret key (non-interactive)")
	s3ConfigCmd.Flags().StringVar(&s3ConfigPrefix, "prefix", "", "S3 key prefix (non-interactive)")
	s3ConfigCmd.Flags().StringVar(&s3ConfigEndpoint, "endpoint", "", "Custom S3-compatible endpoint URL (non-interactive)")
	s3Cmd.AddCommand(s3ConfigCmd)
	s3Cmd.AddCommand(s3ShowCmd)
	if installedInPath {
		settingCmd.AddCommand(s3Cmd)
	}
}
