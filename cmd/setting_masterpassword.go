package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"dbtool/internal/config"
	"dbtool/internal/credvault"

	"github.com/spf13/cobra"
)

var masterPasswordCmd = &cobra.Command{
	Use:   "master-password",
	Short: "Manage the master password protecting saved DB config passwords",
	Long: `The master password protects DB passwords saved on connection configs
(dbtool config add/edit's "save this password" prompt, or --password). It's
set the first time you choose to save a config password, and is never
stored anywhere itself — only a verification record is kept, so a wrong
guess can be detected without ever writing the real password to disk.

This is separate from, and does not affect, credentials used by scheduled
jobs (job passwords, S3 keys, the proxy password, the Telegram bot token) —
those keep working unattended via their own auto-generated key, with no
master password involved, so cron/schedule execution is unaffected by
anything in this command group.`,
}

var masterPasswordStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show whether a master password is set up",
	Run: func(cmd *cobra.Command, args []string) {
		if credvault.IsConfigured() {
			fmt.Println("Master password is set up.")
		} else {
			fmt.Println("No master password set up yet — it will be created the first time you save a config password.")
		}
	},
}

var masterPasswordResetForce bool

var masterPasswordResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Forget the master password and clear every saved config password",
	Long: `Discards the master password's verification record and clears the saved
Password field on every config. This is the recovery path if you forget the
master password: any config passwords saved under the old one are already
unrecoverable at that point (that's the point of encryption — dbtool has
never stored the password itself), so this just cleans up the now-useless
ciphertext and lets you set a new master password next time you save one.

This does NOT touch job/S3/proxy/Telegram credentials — those are unrelated
to the master password.`,
	Run: func(cmd *cobra.Command, args []string) {
		if !credvault.IsConfigured() {
			fmt.Println("No master password is set up — nothing to reset.")
			return
		}

		if !masterPasswordResetForce {
			fmt.Println("This will permanently clear the saved password on every config that has one.")
			fmt.Print("Continue? (y/n): ")
			reader := bufio.NewReader(os.Stdin)
			ans, _ := reader.ReadString('\n')
			ans = strings.TrimSpace(strings.ToLower(ans))
			if ans != "y" && ans != "yes" {
				fmt.Println("Aborted.")
				return
			}
		}

		if err := config.ResetMasterPassword(); err != nil {
			fmt.Println("Failed to reset master password:", err)
			os.Exit(1)
		}
		fmt.Println("Master password reset. Saved config passwords have been cleared.")
	},
}

var masterPasswordChangeCmd = &cobra.Command{
	Use:   "change",
	Short: "Change the master password without losing saved config passwords",
	Long: `Prompts for your current master password (to verify it and decrypt your
existing saved config passwords), then a new one (entered twice, to
confirm), and re-encrypts every saved config password under the new one.

Unlike 'master-password reset', this does NOT clear your saved passwords —
use this when you just want a different master password, and 'reset' only
when you've forgotten the current one and are starting over.`,
	Run: func(cmd *cobra.Command, args []string) {
		if !credvault.IsConfigured() {
			fmt.Println("No master password is set up yet — nothing to change. It will be created the first time you save a config password.")
			return
		}

		if err := config.ChangeMasterPassword(); err != nil {
			fmt.Println("Failed to change master password:", err)
			os.Exit(1)
		}
		fmt.Println("Master password changed.")
	},
}

func init() {
	masterPasswordResetCmd.Flags().BoolVar(&masterPasswordResetForce, "force", false, "Skip the confirmation prompt (non-interactive)")
	masterPasswordCmd.AddCommand(masterPasswordStatusCmd)
	masterPasswordCmd.AddCommand(masterPasswordChangeCmd)
	masterPasswordCmd.AddCommand(masterPasswordResetCmd)
	if installedInPath {
		settingCmd.AddCommand(masterPasswordCmd)
	}
}
