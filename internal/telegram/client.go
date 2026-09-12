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

const apiBase = "https://api.telegram.org"

// maxSendAttempts bounds retries for a single message/document send.
const maxSendAttempts = 3

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
	return withRetries("sendMessage", func() error {
		form := url.Values{
			"chat_id": {chatID},
			"text":    {text},
		}
		resp, err := http.PostForm(fmt.Sprintf("%s/bot%s/sendMessage", apiBase, token), form)
		if err != nil {
			return fmt.Errorf("sendMessage request: %w", err)
		}
		defer resp.Body.Close()
		return checkResponse(resp)
	})
}

// sendDocument uploads a single document (one chunk) to chatID with the given
// filename and caption. newReader is called on every attempt to obtain a
// fresh reader over the chunk's bytes, since a failed attempt may have
// partially consumed the previous one.
func sendDocument(token, chatID string, newReader func() (io.Reader, error), filename, caption string) error {
	return withRetries("sendDocument", func() error {
		r, err := newReader()
		if err != nil {
			return fmt.Errorf("open chunk %s: %w", filename, err)
		}

		var body bytes.Buffer
		mw := multipart.NewWriter(&body)

		if err := mw.WriteField("chat_id", chatID); err != nil {
			return err
		}
		if caption != "" {
			if err := mw.WriteField("caption", caption); err != nil {
				return err
			}
		}
		part, err := mw.CreateFormFile("document", filename)
		if err != nil {
			return err
		}
		if _, err := io.Copy(part, r); err != nil {
			return fmt.Errorf("write chunk %s into request: %w", filename, err)
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
