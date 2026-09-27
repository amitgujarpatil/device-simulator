package apiclient

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"time"
)

type FormFile struct {
	Key      string `json:"key"`
	FileName string `json:"fileName"`
	MimeType string `json:"mimeType"`
	Base64   string `json:"base64"`
}

type SendPayload struct {
	Method      string     `json:"method"`
	URL         string     `json:"url"`
	Headers     []KVPair   `json:"headers"`
	BodyType    string     `json:"bodyType"`
	BodyContent string     `json:"bodyContent"`
	FormPairs   []KVPair   `json:"formPairs"`
	FormFiles   []FormFile `json:"formFiles"`
	TimeoutSec  int        `json:"timeoutSec"`
}

type SendResponse struct {
	Status     int               `json:"status"`
	StatusText string            `json:"statusText"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
	DurationMs int64             `json:"durationMs"`
	SizeBytes  int               `json:"sizeBytes"`
	Error      string            `json:"error"`
}

func Execute(p SendPayload) SendResponse {
	timeout := time.Duration(p.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	var bodyReader io.Reader
	var contentType string

	switch p.BodyType {
	case "raw":
		bodyReader = strings.NewReader(p.BodyContent)
	case "urlencoded":
		vals := url.Values{}
		for _, kv := range p.FormPairs {
			if kv.Enabled && kv.Key != "" {
				vals.Set(kv.Key, kv.Value)
			}
		}
		bodyReader = strings.NewReader(vals.Encode())
		contentType = "application/x-www-form-urlencoded"
	case "form-data":
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		for _, kv := range p.FormPairs {
			if kv.Enabled && kv.Key != "" {
				w.WriteField(kv.Key, kv.Value)
			}
		}
		for _, ff := range p.FormFiles {
			if ff.Key == "" || ff.Base64 == "" {
				continue
			}
			data, err := base64.StdEncoding.DecodeString(ff.Base64)
			if err != nil {
				continue
			}
			mime := ff.MimeType
			if mime == "" {
				mime = "application/octet-stream"
			}
			fileName := ff.FileName
			if fileName == "" {
				fileName = ff.Key
			}
			h := textproto.MIMEHeader{}
			h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, ff.Key, fileName))
			h.Set("Content-Type", mime)
			part, err := w.CreatePart(h)
			if err != nil {
				continue
			}
			part.Write(data)
		}
		w.Close()
		bodyReader = &buf
		contentType = w.FormDataContentType()
	}

	req, err := http.NewRequest(p.Method, p.URL, bodyReader)
	if err != nil {
		return SendResponse{Error: fmt.Sprintf("build request: %s", err)}
	}

	hasContentType := false
	for _, h := range p.Headers {
		if h.Enabled && h.Key != "" {
			req.Header.Set(h.Key, h.Value)
			if strings.EqualFold(h.Key, "content-type") {
				hasContentType = true
			}
		}
	}
	if contentType != "" && !hasContentType {
		req.Header.Set("Content-Type", contentType)
	}

	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start).Milliseconds()
	if err != nil {
		return SendResponse{Error: err.Error(), DurationMs: elapsed}
	}
	defer resp.Body.Close()

	const maxBody = 2 * 1024 * 1024
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	truncated := false
	if len(body) > maxBody {
		body = body[:maxBody]
		truncated = true
	}

	headers := make(map[string]string, len(resp.Header))
	for k, v := range resp.Header {
		headers[k] = strings.Join(v, ", ")
	}

	bodyStr := string(body)
	if truncated {
		bodyStr += "\n\n... [response truncated at 2 MB]"
	}

	return SendResponse{
		Status:     resp.StatusCode,
		StatusText: fmt.Sprintf("%d %s", resp.StatusCode, http.StatusText(resp.StatusCode)),
		Headers:    headers,
		Body:       bodyStr,
		DurationMs: elapsed,
		SizeBytes:  len(body),
	}
}
