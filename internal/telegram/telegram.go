package telegram

import (
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"

	"dbtool/internal/archivecrypt"
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
// chunks no larger than cfg.ChunkSizeBytes(), and delivers them to cfg.ChatID
// via the bot identified by cfg.BotToken: an initial HTML-formatted message
// announces the dump name, size, and part count, then each chunk is sent as
// its own document so the reader can identify and reassemble them later
// (cat *.part* > dump.tar && tar -xf dump.tar).
//
// Chunks are sent one document per message, not grouped into a Telegram
// "album" (sendMediaGroup): that was tried, but the hosted Bot API caps the
// *combined* multipart body of a single request at roughly the same ~50MB
// it caps one file at — so bundling even two chunks anywhere near
// cfg.ChunkSizeBytes()'s default (49MB) into one album request reliably
// fails with 413 Request Entity Too Large. Since dbtool's whole point in
// chunking is to use large chunks (fewer parts, matching Telegram's own
// per-file limit), grouping raw uploads this way isn't viable at a
// meaningful chunk size, so each chunk goes out as an independent request.
func SendDump(cfg settings.TelegramConfig, dumpDir string) error {
	if !cfg.Enabled() {
		return fmt.Errorf("telegram is not configured")
	}

	dumpName := filepath.Base(dumpDir)

	tarPath, _, err := tarDir(dumpDir)
	if err != nil {
		return fmt.Errorf("archive dump: %w", err)
	}
	defer os.Remove(tarPath)

	// archiveExt is the file extension callers need to reassemble parts
	// back into ("dbtool decrypt" for .tar.enc, plain tar -xf for .tar) —
	// it and sendPath both switch to the encrypted file below when
	// encryption is configured.
	archiveExt := "tar"
	sendPath := tarPath

	if cfg.EncryptionEnabled() {
		encPath := tarPath + ".enc"
		if err := encryptFile(encPath, tarPath, cfg.EncryptionPassword); err != nil {
			return fmt.Errorf("encrypt archive: %w", err)
		}
		defer os.Remove(encPath)
		archiveExt = "tar.enc"
		sendPath = encPath
	}

	info, err := os.Stat(sendPath)
	if err != nil {
		return fmt.Errorf("stat archive: %w", err)
	}
	size := info.Size()

	chunkSize := cfg.ChunkSizeBytes()
	ranges := chunkRanges(size, chunkSize)
	total := len(ranges)

	logger.Info("telegram: sending %q as %d chunk(s) of up to %d bytes each", dumpName, total, chunkSize)

	if err := sendMessageHTML(cfg.BotToken, cfg.ChatID, announcementHTML(dumpName, size, total, archiveExt)); err != nil {
		return fmt.Errorf("send announcement: %w", err)
	}

	f, err := os.Open(sendPath)
	if err != nil {
		return fmt.Errorf("open archive for chunking: %w", err)
	}
	defer f.Close()

	for i, r := range ranges {
		partName := fmt.Sprintf("%s.%s.part%03d", dumpName, archiveExt, i+1)
		caption := fmt.Sprintf("%s (part %d/%d)", dumpName, i+1, total)

		section := io.NewSectionReader(f, r.Offset, r.Length)
		newReader := func() (io.Reader, error) {
			if _, err := section.Seek(0, io.SeekStart); err != nil {
				return nil, err
			}
			return section, nil
		}

		fmt.Printf("Sending %s to Telegram (%d/%d)...\n", partName, i+1, total)
		doc := documentUpload{filename: partName, caption: caption, newReader: newReader}
		if err := sendDocument(cfg.BotToken, cfg.ChatID, doc); err != nil {
			return fmt.Errorf("send chunk %d/%d: %w", i+1, total, err)
		}
	}

	logger.Info("telegram: finished sending %q (%d chunk(s))", dumpName, total)
	return nil
}

// announcementHTML builds the Telegram-HTML (parse_mode=HTML) announcement
// message describing dumpName's size and part count, plus the shell
// command to reassemble the parts once downloaded. archiveExt is "tar" or
// "tar.enc" (see SendDump) — an encrypted archive gets an extra "dbtool
// decrypt" step in the reassemble instructions, since Telegram delivery
// has no automated ingest path back into dbtool; the operator reassembles
// and decrypts by hand before extracting.
func announcementHTML(dumpName string, size int64, total int, archiveExt string) string {
	name := html.EscapeString(dumpName)

	reassembleCmd := fmt.Sprintf("cat %s.%s.part* > %s.%s", dumpName, archiveExt, dumpName, archiveExt)
	if archiveExt == "tar.enc" {
		reassembleCmd += fmt.Sprintf(" && dbtool decrypt %s.tar.enc %s.tar", dumpName, dumpName)
	}
	reassembleCmd += fmt.Sprintf(" && tar -xf %s.tar", dumpName)

	return fmt.Sprintf(
		"📦 <b>dbtool backup</b>\n<b>Name:</b> <code>%s</code>\n<b>Size:</b> %.2f MB\n<b>Parts:</b> %d\n\n<b>Reassemble with:</b>\n<pre>%s</pre>",
		name, float64(size)/(1024*1024), total, html.EscapeString(reassembleCmd),
	)
}

// encryptFile encrypts the file at srcPath under password, writing the
// result to destPath.
func encryptFile(destPath, srcPath, password string) error {
	src, err := os.Open(srcPath) // #nosec G304 -- srcPath is dbtool's own just-created tar temp file, not user input
	if err != nil {
		return fmt.Errorf("open %s: %w", srcPath, err)
	}
	defer src.Close()

	dest, err := os.Create(destPath) // #nosec G304 -- destPath is derived from dbtool's own temp file path, not user input
	if err != nil {
		return fmt.Errorf("create %s: %w", destPath, err)
	}

	if err := archivecrypt.EncryptStream(dest, src, password); err != nil {
		dest.Close()
		return err
	}
	return dest.Close()
}
