package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"dbtool/internal/interactivelist"
	"dbtool/internal/logger"
	"dbtool/internal/secureinput"
	"dbtool/internal/settings"
	"dbtool/internal/telegram"

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
				fmt.Println("--chat-id is required.")
				os.Exit(1)
			}
			token, err := secureinput.ResolveSecret(telegramSetToken, "DBTOOL_TELEGRAM_TOKEN", "Bot token: ")
			if err != nil {
				fmt.Println("Error reading bot token:", err)
				os.Exit(1)
			}
			s.Telegram.BotToken = token
			s.Telegram.ChatID = telegramSetChatID
			if cmd.Flags().Changed("chunk-size-mb") {
				if telegramSetChunkSizeMB <= 0 {
					fmt.Println("--chunk-size-mb must be a positive integer.")
					os.Exit(1)
				}
				s.Telegram.ChunkSizeMB = telegramSetChunkSizeMB
			}
			if cmd.Flags().Changed("encryption-password") {
				s.Telegram.EncryptionPassword = telegramSetEncryptionPassword
			}
		} else {
			reader := bufio.NewReader(os.Stdin)

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
			fmt.Print(interactivelist.PromptLabel("Chunk size in MB", curChunk))
			chunkStr, _ := reader.ReadString('\n')
			chunkStr = strings.TrimSpace(chunkStr)
			if chunkStr != "" {
				n, err := strconv.Atoi(chunkStr)
				if err != nil || n <= 0 {
					fmt.Println("Invalid chunk size; keeping previous value.")
				} else {
					s.Telegram.ChunkSizeMB = n
				}
			}

			s.Telegram.EncryptionPassword = readSecret("Archive encryption password (optional — encrypts the archive before sending; leave blank to disable)", s.Telegram.EncryptionPassword)

			if s.Telegram.BotToken == "" || s.Telegram.ChatID == "" {
				fmt.Println("Error: bot token and chat ID are required.")
				os.Exit(1)
			}
		}

		if err := settings.Save(s); err != nil {
			logger.Error("failed to save telegram settings: %v", err)
			fmt.Println("Error saving settings:", err)
			os.Exit(1)
		}

		logger.Info("telegram settings saved (chat_id=%s)", s.Telegram.ChatID)
		fmt.Println("Telegram settings saved.")
	},
}

var telegramShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the current Telegram delivery configuration",
	Run: func(cmd *cobra.Command, args []string) {
		s := settings.Load()
		if !s.Telegram.Enabled() {
			fmt.Println("No Telegram bot configured.")
			return
		}
		fmt.Printf("Bot token:  %s\n", maskedPlaceholder)
		fmt.Printf("Chat ID:    %s\n", s.Telegram.ChatID)
		fmt.Printf("Chunk size: %d MB\n", s.Telegram.ChunkSizeBytes()/(1024*1024))
		if s.Telegram.EncryptionEnabled() {
			fmt.Println("Encryption: enabled")
		}
	},
}

var telegramClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Remove the Telegram delivery configuration",
	Run: func(cmd *cobra.Command, args []string) {
		s := settings.Load()
		s.Telegram = settings.TelegramConfig{}
		if err := settings.Save(s); err != nil {
			logger.Error("failed to clear telegram settings: %v", err)
			fmt.Println("Error saving settings:", err)
			os.Exit(1)
		}
		logger.Info("telegram settings cleared")
		fmt.Println("Telegram configuration cleared.")
	},
}

var telegramTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Send a test message to verify the bot token and chat ID",
	Run: func(cmd *cobra.Command, args []string) {
		s := settings.Load()
		if !s.Telegram.Enabled() {
			fmt.Println("No Telegram bot configured. Run 'dbtool setting telegram set' first.")
			os.Exit(1)
		}
		if err := telegram.SendTestMessage(s.Telegram); err != nil {
			logger.Error("telegram test message failed: %v", err)
			fmt.Println("Failed to send test message:", err)
			os.Exit(1)
		}
		fmt.Println("Test message sent successfully.")
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
	if installedInPath {
		settingCmd.AddCommand(telegramCmd)
	}
}
