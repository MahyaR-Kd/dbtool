package telegram

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"dbtool/internal/logger"
	"dbtool/internal/settings"
)

// SendTestMessage sends a short text message to cfg.ChatID to verify that the
// bot token and chat ID are valid before relying on them during a real dump.
func SendTestMessage(cfg settings.TelegramConfig) error {
	if !cfg.Enabled() {
		return fmt.Errorf("telegram is not configured")
	}
	return sendMessage(cfg.BotToken, cfg.ChatID, "dbtool: this is a test message. Telegram delivery is configured correctly.")
}

// SendDump bundles every file in dumpDir into a tar archive, splits it into
// chunks no larger than cfg.ChunkSizeBytes(), and delivers each chunk to
// cfg.ChatID as a Telegram document via the bot identified by cfg.BotToken.
// An initial text message announces the dump name, size, and part count so
// the chunks can be identified and reassembled later
// (cat *.part* > dump.tar && tar -xf dump.tar).
func SendDump(cfg settings.TelegramConfig, dumpDir string) error {
	if !cfg.Enabled() {
		return fmt.Errorf("telegram is not configured")
	}

	dumpName := filepath.Base(dumpDir)

	tarPath, size, err := tarDir(dumpDir)
	if err != nil {
		return fmt.Errorf("archive dump: %w", err)
	}
	defer os.Remove(tarPath)

	chunkSize := cfg.ChunkSizeBytes()
	ranges := chunkRanges(size, chunkSize)
	total := len(ranges)

	logger.Info("telegram: sending %q as %d chunk(s) of up to %d bytes each", dumpName, total, chunkSize)

	announcement := fmt.Sprintf(
		"dbtool backup: %s\nSize: %.2f MB\nParts: %d\n\nReassemble with:\ncat %s.tar.part* > %s.tar && tar -xf %s.tar",
		dumpName, float64(size)/(1024*1024), total, dumpName, dumpName, dumpName,
	)
	if err := sendMessage(cfg.BotToken, cfg.ChatID, announcement); err != nil {
		return fmt.Errorf("send announcement: %w", err)
	}

	f, err := os.Open(tarPath)
	if err != nil {
		return fmt.Errorf("open tar for chunking: %w", err)
	}
	defer f.Close()

	for i, r := range ranges {
		partName := fmt.Sprintf("%s.tar.part%03d", dumpName, i+1)
		caption := fmt.Sprintf("%s (part %d/%d)", dumpName, i+1, total)

		section := io.NewSectionReader(f, r.Offset, r.Length)
		newReader := func() (io.Reader, error) {
			if _, err := section.Seek(0, io.SeekStart); err != nil {
				return nil, err
			}
			return section, nil
		}

		fmt.Printf("Sending %s to Telegram (%d/%d)...\n", partName, i+1, total)
		if err := sendDocument(cfg.BotToken, cfg.ChatID, newReader, partName, caption); err != nil {
			return fmt.Errorf("send chunk %d/%d: %w", i+1, total, err)
		}
	}

	logger.Info("telegram: finished sending %q (%d chunk(s))", dumpName, total)
	return nil
}
