package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"time"

	"dbtool/internal/logger"
)

// apiBase is a var (not a const) so tests can point it at an httptest
// server instead of the real Telegram API.
var apiBase = "https://api.telegram.org"

// maxSendAttempts bounds retries for a single message/document send.
const maxSendAttempts = 3

// maxMediaGroupItems is Telegram's hard cap on how many items a single
// sendMediaGroup call may contain (also enforces a minimum of 2 — a group
// of 1 isn't valid, so a lone leftover chunk falls back to sendDocument).
const maxMediaGroupItems = 10

// documentUpload describes one file to deliver as a Telegram document,
// either individually (sendDocument) or grouped into an album
// (sendMediaGroup) so multiple parts appear as a single block in the chat
// instead of as separate messages. newReader is called fresh on every send
// attempt, since a failed attempt may have partially consumed the reader
// from a previous one.
type documentUpload struct {
	filename  string
	caption   string
	newReader func() (io.Reader, error)
}

// apiResponse mirrors the subset of the Telegram Bot API response envelope
// that callers need to check for success.
type apiResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

// sendMessage posts a plain text message to chatID.
func sendMessage(token, chatID, text string) error {
	return sendMessageWithParseMode(token, chatID, text, "")
}

// sendMessageHTML posts a message formatted with Telegram's HTML parse
// mode (bold, <code>, <pre>, …) instead of plain text.
func sendMessageHTML(token, chatID, text string) error {
	return sendMessageWithParseMode(token, chatID, text, "HTML")
}

func sendMessageWithParseMode(token, chatID, text, parseMode string) error {
	return withRetries("sendMessage", func() error {
		form := url.Values{
			"chat_id": {chatID},
			"text":    {text},
		}
		if parseMode != "" {
			form.Set("parse_mode", parseMode)
		}
		resp, err := http.PostForm(fmt.Sprintf("%s/bot%s/sendMessage", apiBase, token), form)
		if err != nil {
			return fmt.Errorf("sendMessage request: %w", err)
		}
		defer resp.Body.Close()
		return checkResponse(resp)
	})
}

// sendDocument uploads a single document to chatID. newReader is called on
// every attempt to obtain a fresh reader over the document's bytes, since a
// failed attempt may have partially consumed the previous one.
func sendDocument(token, chatID string, doc documentUpload) error {
	return withRetries("sendDocument", func() error {
		r, err := doc.newReader()
		if err != nil {
			return fmt.Errorf("open %s: %w", doc.filename, err)
		}

		var body bytes.Buffer
		mw := multipart.NewWriter(&body)

		if err := mw.WriteField("chat_id", chatID); err != nil {
			return err
		}
		if doc.caption != "" {
			if err := mw.WriteField("caption", doc.caption); err != nil {
				return err
			}
		}
		part, err := mw.CreateFormFile("document", doc.filename)
		if err != nil {
			return err
		}
		if _, err := io.Copy(part, r); err != nil {
			return fmt.Errorf("write %s into request: %w", doc.filename, err)
		}
		if err := mw.Close(); err != nil {
			return err
		}

		req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/bot%s/sendDocument", apiBase, token), &body)
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", mw.FormDataContentType())

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("sendDocument request: %w", err)
		}
		defer resp.Body.Close()
		return checkResponse(resp)
	})
}

// sendMediaGroup uploads 2-10 documents as a single grouped "album"
// message, so Telegram displays them together as one block in the chat
// instead of as separate messages — the same way a phone's Telegram app
// groups multiple photos/files shared at once. docs must contain between
// 2 and maxMediaGroupItems entries; a single document must go through
// sendDocument instead, since Telegram rejects a media group of one.
func sendMediaGroup(token, chatID string, docs []documentUpload) error {
	if len(docs) < 2 || len(docs) > maxMediaGroupItems {
		return fmt.Errorf("sendMediaGroup: got %d document(s), need 2-%d", len(docs), maxMediaGroupItems)
	}

	return withRetries("sendMediaGroup", func() error {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)

		if err := mw.WriteField("chat_id", chatID); err != nil {
			return err
		}

		type mediaEntry struct {
			Type    string `json:"type"`
			Media   string `json:"media"`
			Caption string `json:"caption,omitempty"`
		}
		media := make([]mediaEntry, len(docs))

		for i, doc := range docs {
			field := fmt.Sprintf("file%d", i)
			r, err := doc.newReader()
			if err != nil {
				return fmt.Errorf("open %s: %w", doc.filename, err)
			}
			part, err := mw.CreateFormFile(field, doc.filename)
			if err != nil {
				return err
			}
			if _, err := io.Copy(part, r); err != nil {
				return fmt.Errorf("write %s into request: %w", doc.filename, err)
			}
			media[i] = mediaEntry{Type: "document", Media: "attach://" + field, Caption: doc.caption}
		}

		mediaJSON, err := json.Marshal(media)
		if err != nil {
			return fmt.Errorf("encode media group: %w", err)
		}
		if err := mw.WriteField("media", string(mediaJSON)); err != nil {
			return err
		}
		if err := mw.Close(); err != nil {
			return err
		}

		req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/bot%s/sendMediaGroup", apiBase, token), &body)
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", mw.FormDataContentType())

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("sendMediaGroup request: %w", err)
		}
		defer resp.Body.Close()
		return checkResponse(resp)
	})
}

// checkResponse reads and validates the Telegram API response envelope,
// returning a retryableError when the API reports a rate limit.
func checkResponse(resp *http.Response) error {
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	var parsed apiResponse
	if jsonErr := json.Unmarshal(data, &parsed); jsonErr != nil {
		return fmt.Errorf("telegram API returned unparseable response (status %d): %s", resp.StatusCode, string(data))
	}

	if parsed.OK {
		return nil
	}

	if resp.StatusCode == http.StatusTooManyRequests && parsed.Parameters.RetryAfter > 0 {
		return retryableError{
			err:        fmt.Errorf("telegram API rate limited: %s", parsed.Description),
			retryAfter: time.Duration(parsed.Parameters.RetryAfter) * time.Second,
		}
	}

	return fmt.Errorf("telegram API error (status %d): %s", resp.StatusCode, parsed.Description)
}

// retryableError signals that withRetries should wait retryAfter (if set)
// before trying again.
type retryableError struct {
	err        error
	retryAfter time.Duration
}

func (e retryableError) Error() string { return e.err.Error() }

// withRetries runs fn up to maxSendAttempts times, backing off between
// attempts (honoring a server-specified retryAfter when present).
func withRetries(op string, fn func() error) error {
	var lastErr error
	for attempt := 1; attempt <= maxSendAttempts; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}
		lastErr = err

		if attempt == maxSendAttempts {
			break
		}

		wait := time.Duration(attempt) * 2 * time.Second
		if re, ok := err.(retryableError); ok && re.retryAfter > 0 {
			wait = re.retryAfter
		}
		logger.Warn("telegram %s attempt %d/%d failed: %v (retrying in %s)", op, attempt, maxSendAttempts, err, wait)
		time.Sleep(wait)
	}
	return fmt.Errorf("telegram %s failed after %d attempts: %w", op, maxSendAttempts, lastErr)
}
