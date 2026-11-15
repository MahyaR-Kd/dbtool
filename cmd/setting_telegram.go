package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/MahyaR-Kd/dbtool/internal/interactivelist"
	"github.com/MahyaR-Kd/dbtool/internal/logger"
	"github.com/MahyaR-Kd/dbtool/internal/secureinput"
	"github.com/MahyaR-Kd/dbtool/internal/settings"
	"github.com/MahyaR-Kd/dbtool/internal/telegram"
	"github.com/MahyaR-Kd/dbtool/internal/ui"

	"github.com/spf13/cobra"
)

var telegramCmd = &cobra.Command{
	Use:   "telegram",
	Short: "Manage Telegram delivery settings for dumps",
}

var telegramSetToken string
var telegramSetChatID string
var telegramSetChunkSizeMB int
var telegramSetEncryptionPassword string

var telegramSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Configure the Telegram bot used to deliver dump archives",
	Long: `Configure a Telegram bot that dbtool sends dump archives to.

After every dump (regardless of whether it's stored locally or on S3), if a
Telegram bot is configured the dump directory is bundled into a tar archive,
split into chunks that respect Telegram's per-file size limit, and each chunk
is sent as a document to the configured chat.

Non-interactive example:
  dbtool setting telegram set --token 123456:ABC-DEF --chat-id 987654321
  dbtool setting telegram set --token 123456:ABC-DEF --chat-id 987654321 --chunk-size-mb 40`,
	Run: func(cmd *cobra.Command, args []string) {
		s := settings.Load()

		hasFlags := cmd.Flags().Changed("token") ||
			cmd.Flags().Changed("chat-id") ||
			cmd.Flags().Changed("chunk-size-mb") ||
			cmd.Flags().Changed("encryption-password")

		if hasFlags {
			if !cmd.Flags().Changed("chat-id") {
				ui.Fatal("--chat-id is required.")
			}
			token, err := secureinput.ResolveSecret(telegramSetToken, "DBTOOL_TELEGRAM_TOKEN", "Bot token: ")
			if err != nil {
				ui.Fatal("Could not read bot token: %v", err)
			}
			s.Telegram.BotToken = token
			s.Telegram.ChatID = telegramSetChatID
			if cmd.Flags().Changed("chunk-size-mb") {
				if telegramSetChunkSizeMB <= 0 {
					ui.Fatal("--chunk-size-mb must be a positive integer.")
				}
				s.Telegram.ChunkSizeMB = telegramSetChunkSizeMB
			}
			if cmd.Flags().Changed("encryption-password") {
				s.Telegram.EncryptionPassword = telegramSetEncryptionPassword
			}
		} else {
			reader := secureinput.Stdin()

			readSecret := func(prompt, cur string) string {
				placeholder := ""
				if cur != "" {
					placeholder = maskedPlaceholder
				}
				val, err := secureinput.ReadPassword(interactivelist.PromptLabel(prompt, placeholder))
				if err != nil {
					ui.Error("Could not read input: %v", err)
					return cur
				}
				val = strings.TrimSpace(val)
				if val == "" || val == maskedPlaceholder {
					return cur
				}
				return val
			}

			s.Telegram.BotToken = readSecret("Bot token", s.Telegram.BotToken)
			s.Telegram.ChatID = interactivelist.Text(reader, "Chat ID", s.Telegram.ChatID)

			curChunk := strconv.Itoa(s.Telegram.ChunkSizeMB)
			if s.Telegram.ChunkSizeMB <= 0 {
				curChunk = fmt.Sprintf("default, %d", settings.DefaultTelegramChunkSizeMB)
			}
			ui.Print(interactivelist.PromptLabel("Chunk size in MB", curChunk))
			chunkStr, _ := reader.ReadString('\n')
			chunkStr = strings.TrimSpace(chunkStr)
			if chunkStr != "" {
				n, err := strconv.Atoi(chunkStr)
				if err != nil || n <= 0 {
					ui.Warn("Invalid chunk size; keeping previous value.")
				} else {
					s.Telegram.ChunkSizeMB = n
				}
			}

			s.Telegram.EncryptionPassword = readSecret("Archive encryption password (optional — encrypts the archive before sending; leave blank to disable)", s.Telegram.EncryptionPassword)

			if s.Telegram.BotToken == "" || s.Telegram.ChatID == "" {
				ui.Fatal("Bot token and chat ID are required.")
			}
		}
		s.Telegram.Disabled = false

		if err := settings.Save(s); err != nil {
			logger.Error("failed to save telegram settings: %v", err)
			ui.Fatal("Could not save settings: %v", err)
		}

		logger.Info("telegram settings saved (chat_id=%s)", s.Telegram.ChatID)
		ui.Success("Telegram settings saved.")
	},
}

var telegramShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the current Telegram delivery configuration",
	Run: func(cmd *cobra.Command, args []string) {
		s := settings.Load()
		if !s.Telegram.Configured() {
			ui.Empty("No Telegram bot configured", "Set one up with "+ui.Code("dbtool setting telegram set"))
			return
		}
		ui.Println(ui.Bold("Telegram delivery"))
		ui.PrintKeyValues(
			ui.KV{Key: "Status", Value: enabledStatus(s.Telegram.Enabled())},
			ui.KV{Key: "Bot token", Value: maskedPlaceholder},
			ui.KV{Key: "Chat ID", Value: s.Telegram.ChatID},
			ui.KV{Key: "Chunk size", Value: fmt.Sprintf("%d MB", s.Telegram.ChunkSizeBytes()/(1024*1024))},
			ui.KV{Key: "Encryption", Value: enabledStatus(s.Telegram.EncryptionEnabled())},
		)
	},
}

var telegramEnableCmd = featureToggleCommand("enable", "Enable Telegram delivery", func(s *settings.Settings) (bool, string) {
	if !s.Telegram.Configured() {
		return false, "No Telegram bot configured. Run 'dbtool setting telegram set' first."
	}
	s.Telegram.Disabled = false
	return true, "Telegram delivery enabled."
})

var telegramDisableCmd = featureToggleCommand("disable", "Disable Telegram delivery without removing its settings", func(s *settings.Settings) (bool, string) {
	if !s.Telegram.Configured() {
		return false, "No Telegram bot configured."
	}
	s.Telegram.Disabled = true
	return true, "Telegram delivery disabled."
})

var telegramClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Remove the Telegram delivery configuration",
	Run: func(cmd *cobra.Command, args []string) {
		s := settings.Load()
		s.Telegram = settings.TelegramConfig{}
		if err := settings.Save(s); err != nil {
			logger.Error("failed to clear telegram settings: %v", err)
			ui.Fatal("Could not save settings: %v", err)
		}
		logger.Info("telegram settings cleared")
		ui.Success("Telegram configuration cleared.")
	},
}

var telegramTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Send a test message to verify the bot token and chat ID",
	Run: func(cmd *cobra.Command, args []string) {
		s := settings.Load()
		if !s.Telegram.Configured() {
			fatalWithHint("No Telegram bot configured. Run 'dbtool setting telegram set' first.")
		}
		// Testing validates the saved credentials even while routine delivery is disabled.
		testConfig := s.Telegram
		testConfig.Disabled = false
		t := ui.StartTask("Sending test message to chat %s", s.Telegram.ChatID)
		if err := telegram.SendTestMessage(testConfig); err != nil {
			logger.Error("telegram test message failed: %v", err)
			t.Fail(err)
			os.Exit(1)
		}
		t.Done("sent successfully")
	},
}

func init() {
	telegramSetCmd.Flags().StringVar(&telegramSetToken, "token", "", "Telegram bot token (non-interactive; falls back to DBTOOL_TELEGRAM_TOKEN env var, then a masked prompt)")
	telegramSetCmd.Flags().StringVar(&telegramSetChatID, "chat-id", "", "Telegram chat ID to deliver dumps to (non-interactive)")
	telegramSetCmd.Flags().IntVar(&telegramSetChunkSizeMB, "chunk-size-mb", 0, "Chunk size in MB, defaults to 49 (non-interactive)")
	telegramSetCmd.Flags().StringVar(&telegramSetEncryptionPassword, "encryption-password", "", "Password to encrypt the archive with before sending (non-interactive; empty disables encryption)")
	telegramCmd.AddCommand(telegramSetCmd)
	telegramCmd.AddCommand(telegramShowCmd)
	telegramCmd.AddCommand(telegramClearCmd)
	telegramCmd.AddCommand(telegramTestCmd)
	telegramCmd.AddCommand(telegramEnableCmd, telegramDisableCmd)
	if installedInPath {
		settingCmd.AddCommand(telegramCmd)
	}
}
