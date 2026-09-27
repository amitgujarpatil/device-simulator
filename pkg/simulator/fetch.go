package simulator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"
)

func identifyPacketType(p map[string]interface{}) string {
	if p == nil {
		return "unknown"
	}
	if _, ok := p["cv"]; ok {
		return "handshake"
	}
	if _, ok := p["tv"]; ok {
		return "handshake"
	}
	if _, ok := p["GA"]; ok {
		return "gps"
	}
	if _, ok := p["GD"]; ok {
		return "gps"
	}
	if _, ok := p["GT"]; ok {
		return "gps"
	}
	if _, ok := p["P"]; ok {
		return "obd"
	}
	if _, ok := p["DT_UDS3"]; ok {
		return "obd"
	}
	if _, ok := p["DT_UDS"]; ok {
		return "obd"
	}
	if _, ok := p["set"]; ok {
		return "settings"
	}
	return "unknown"
}

func convertToL1Packet(pkt map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(pkt)+1)
	for k, v := range pkt {
		if k != "file" {
			out[k] = v
		}
	}
	out["l"] = "1"
	return out
}

func fetchTelemetry(ctx context.Context, cfg Config, emit func(SimEvent), startT time.Time) ([]Packet, error) {
	psize := cfg.APIPageSize
	if psize <= 0 {
		psize = 1000
	}
	delay := cfg.APIRequestDelay
	if delay <= 0 {
		delay = 300
	}
	maxRetries := 3

	emit(SimEvent{
		Elapsed: time.Since(startT).Milliseconds(),
		Tag:     "FETCH",
		Cls:     "fe",
		Msg:     fmt.Sprintf("Fetching OBD+GPS from API → %s", cfg.SrcIMEI),
		Ty:      "info",
		Step:    "fetch:start",
		Data:    map[string]interface{}{"srcImei": cfg.SrcIMEI, "fromMs": cfg.FromMS, "untilMs": cfg.UntilMS},
	})

	var allRows []Packet
	page := 0
	var lastKey map[string]interface{}
	var prevLastKeyStr string
	client := &http.Client{Timeout: 30 * time.Second}

	for {
		select {
		case <-ctx.Done():
			return allRows, ctx.Err()
		default:
		}
		page++

		url := fmt.Sprintf("%s/idevice/logsV2/%s?psize=%d&token=%s&from=%d&until=%d",
			cfg.APIBase, cfg.SrcIMEI, psize, cfg.APIToken, cfg.FromMS, cfg.UntilMS)
		if lastKey != nil {
			if t, ok := lastKey["t"]; ok {
				url += fmt.Sprintf("&last_t=%v", t)
			}
		}

		var body []byte
		var fetchErr error
		for attempt := 1; attempt <= maxRetries; attempt++ {
			req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
			if err != nil {
				fetchErr = err
				break
			}
			resp, err := client.Do(req)
			if err != nil {
				fetchErr = err
				if attempt < maxRetries {
					time.Sleep(2 * time.Second)
				}
				continue
			}
			body, err = io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				fetchErr = err
				if attempt < maxRetries {
					time.Sleep(2 * time.Second)
				}
				continue
			}
			fetchErr = nil
			break
		}
		if fetchErr != nil {
			return allRows, fmt.Errorf("page %d: %w", page, fetchErr)
		}

		var result struct {
			Logs             []json.RawMessage      `json:"logs"`
			LastEvaluatedKey map[string]interface{} `json:"last_evaluated_key"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return allRows, fmt.Errorf("parse page %d: %w", page, err)
		}

		prevLen := len(allRows)
		for _, rawLog := range result.Logs {
			var entry struct {
				T int64           `json:"t"`
				M json.RawMessage `json:"m"`
			}
			if err := json.Unmarshal(rawLog, &entry); err != nil {
				continue
			}

			var telArr []map[string]interface{}
			// M can be a JSON string (containing array) or a direct array
			var mStr string
			if err := json.Unmarshal(entry.M, &mStr); err == nil {
				// It's a string — parse it as JSON array
				json.Unmarshal([]byte(mStr), &telArr)
			} else {
				// Try as direct array first
				if err2 := json.Unmarshal(entry.M, &telArr); err2 != nil {
					// Try as single object
					var single map[string]interface{}
					if err3 := json.Unmarshal(entry.M, &single); err3 == nil {
						telArr = []map[string]interface{}{single}
					}
				}
			}

			for _, pkt := range telArr {
				t := identifyPacketType(pkt)
				if t != "obd" && t != "gps" {
					continue
				}
				allRows = append(allRows, Packet{T: entry.T, Packet: pkt})
			}
		}

		newCount := len(allRows) - prevLen
		emit(SimEvent{
			Elapsed: time.Since(startT).Milliseconds(),
			Tag:     "FETCH",
			Cls:     "fe",
			Msg:     fmt.Sprintf("Page %d  %d entries (total: %d)", page, len(result.Logs), len(allRows)),
			Ty:      "info",
			Step:    "fetch:page",
			Data:    map[string]interface{}{"page": page, "count": len(result.Logs), "total": len(allRows)},
		})
		_ = newCount

		if len(result.Logs) == 0 {
			break
		}
		if len(result.Logs) == 1 && result.LastEvaluatedKey == nil {
			break
		}

		lekBytes, _ := json.Marshal(result.LastEvaluatedKey)
		lekStr := string(lekBytes)
		if result.LastEvaluatedKey == nil || lekStr == prevLastKeyStr {
			break
		}
		prevLastKeyStr = lekStr
		lastKey = result.LastEvaluatedKey

		if delay > 0 {
			select {
			case <-ctx.Done():
				return allRows, ctx.Err()
			case <-time.After(time.Duration(delay) * time.Millisecond):
			}
		}
	}

	// Sort by timestamp
	sort.Slice(allRows, func(i, j int) bool { return allRows[i].T < allRows[j].T })

	emit(SimEvent{
		Elapsed: time.Since(startT).Milliseconds(),
		Tag:     "FETCH",
		Cls:     "fe",
		Msg:     fmt.Sprintf("Done: %d OBD+GPS packets fetched", len(allRows)),
		Ty:      "ok",
		Step:    "fetch:done",
		Data:    map[string]interface{}{"packets": len(allRows), "pages": page},
	})
	return allRows, nil
}
