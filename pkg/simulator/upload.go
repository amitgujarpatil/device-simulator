package simulator

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func uploadFile(filePath string, seqID int, cfg Config) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return fmt.Errorf("create form file: %w", err)
	}
	if _, err = io.Copy(part, f); err != nil {
		return fmt.Errorf("copy file: %w", err)
	}
	w.Close()

	req, err := http.NewRequest("POST", cfg.UploadURL, &buf)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("intangles-user-token", cfg.UserToken)
	req.Header.Set("imei", cfg.TgtIMEI)
	req.Header.Set("session-token", cfg.SessionToken)
	req.Header.Set("seq-id", fmt.Sprintf("%d", seqID))
	if cfg.EncryptEnabled {
		req.Header.Set("is-file-encrypted", "true")
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("upload request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		maxLen := 200
		if len(body) < maxLen {
			maxLen = len(body)
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body[:maxLen]))
	}
	return nil
}
