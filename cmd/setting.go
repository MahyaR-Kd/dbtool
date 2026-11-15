package cmd

import (
	"github.com/MahyaR-Kd/dbtool/internal/credvault"
	"github.com/MahyaR-Kd/dbtool/internal/settings"
	"github.com/MahyaR-Kd/dbtool/internal/ui"

	"github.com/spf13/cobra"
)

var settingCmd = &cobra.Command{
	Use:   "setting",
	Short: "Manage dbtool settings (storage, working directory, proxy)",
	Long: `Manage dbtool settings (storage, working directory, proxy, Telegram,
master password).

Run without a subcommand to print an overview of the current settings.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		printSettingsOverview(settings.Load())
	},
}

// printSettingsOverview prints a one-screen summary of every setting group.
// Secrets are never shown — only whether they are set.
func printSettingsOverview(s settings.Settings) {
	storage := string(settings.StorageLocal)
	if s.StorageType == settings.StorageS3 {
		storage = "s3://" + s.S3.Bucket
		if s.S3.Prefix != "" {
			storage += "/" + s.S3.Prefix
		}
		if !s.S3Enabled() {
			storage += " " + ui.Muted("(disabled — dumps use local storage)")
		}
	}

	proxy := ui.OnOff(s.Proxy.Enabled())
	if s.Proxy.Configured() {
		proxy += "  " + ui.Muted(s.Proxy.Host+":"+s.Proxy.Port)
	}

	telegram := ui.OnOff(s.Telegram.Enabled())
	if s.Telegram.ChatID != "" {
		telegram += "  " + ui.Muted("chat "+s.Telegram.ChatID)
	}

	ui.Println(ui.Bold("Settings"))
	ui.PrintKeyValues(
		ui.KV{Key: "Work dir", Value: s.WorkDir},
		ui.KV{Key: "Storage", Value: storage},
		ui.KV{Key: "S3 encryption", Value: ui.OnOff(s.S3.EncryptionEnabled())},
		ui.KV{Key: "SOCKS5 proxy", Value: proxy},
		ui.KV{Key: "Telegram", Value: telegram},
		ui.KV{Key: "Telegram encryption", Value: ui.OnOff(s.Telegram.EncryptionEnabled())},
		ui.KV{Key: "Master password", Value: ui.Status(credvault.IsConfigured(), masterPasswordLabel(credvault.IsConfigured()))},
	)
	ui.Hint("Change one with %s", ui.Code("dbtool setting <name> …"))
}

func masterPasswordLabel(set bool) string {
	if set {
		return "set"
	}
	return "not set"
}

func init() {
	if installedInPath {
		rootCmd.AddCommand(settingCmd)
	}
}
