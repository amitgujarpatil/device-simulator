package simulator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
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

// toNormalPacket strips the "l" (L1) field from a packet so Phase 2 always
// publishes GPS as normal packets even if the source data contained l:"1".
func toNormalPacket(pkt map[string]interface{}) map[string]interface{} {
	if _, hasL := pkt["l"]; !hasL {
		return pkt
	}
	out := make(map[string]interface{}, len(pkt))
	for k, v := range pkt {
		if k != "l" {
			out[k] = v
		}
	}
	return out
}

func fetchTelemetry(ctx context.Context, cfg Config, emit func(SimEvent), startT time.Time) ([]Packet, int, error) {
	psize := cfg.APIPageSize
	if psize <= 0 {
		psize = 800
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
	var lastSeenT int64
	var lastBoundaryRaw string // raw JSON of last entry on previous page (the one API re-sends)
	client := &http.Client{Timeout: 30 * time.Second}

	for {
		select {
		case <-ctx.Done():
			return allRows, page, ctx.Err()
		default:
		}
		page++

		srcBase := cfg.SrcAPIBase
		if srcBase == "" {
			srcBase = cfg.APIBase
		}
		if srcBase != "" && !strings.HasPrefix(srcBase, "http://") && !strings.HasPrefix(srcBase, "https://") {
			srcBase = "https://" + srcBase
		}
		srcToken := cfg.SrcAPIToken
		if srcToken == "" {
			srcToken = cfg.APIToken
		}
		url := fmt.Sprintf("%s/idevice/logsV2/%s?psize=%d&token=%s&from=%d&until=%d",
			srcBase, cfg.SrcIMEI, psize, srcToken, cfg.FromMS, cfg.UntilMS)
		if lastKey != nil {
			if t, ok := lastKey["t"]; ok {
				var lastT int64
				switch v := t.(type) {
				case float64:
					lastT = int64(v)
				case json.Number:
					lastT, _ = v.Int64()
				}
				if lastT > 0 {
					url += fmt.Sprintf("&last_t=%d", lastT)
				}
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
			return allRows, page, fmt.Errorf("page %d: %w", page, fetchErr)
		}

		var result struct {
			Logs             []json.RawMessage      `json:"logs"`
			LastEvaluatedKey map[string]interface{} `json:"last_evaluated_key"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return allRows, page, fmt.Errorf("parse page %d: %w", page, err)
		}

		// Snapshot boundary from the previous page. We skip only the ONE exact
		// entry the API re-sends (inclusive last_t). Using raw JSON equality
		// means entries at the same timestamp that are genuinely new pass through.
		prevPageMaxT := lastSeenT
		prevBoundaryRaw := lastBoundaryRaw
		boundarySkipped := false
		newCount := 0
		for _, rawLog := range result.Logs {
			rawStr := string(rawLog)
			var entry struct {
				T int64           `json:"t"`
				M json.RawMessage `json:"m"`
			}
			if err := json.Unmarshal(rawLog, &entry); err != nil {
				continue
			}
			// Skip the exact boundary entry the API re-sends (once only).
			if !boundarySkipped && entry.T <= prevPageMaxT && rawStr == prevBoundaryRaw {
				boundarySkipped = true
				continue
			}

			var telArr []map[string]interface{}
			var mStr string
			if err := json.Unmarshal(entry.M, &mStr); err == nil {
				json.Unmarshal([]byte(mStr), &telArr)
			} else {
				if err2 := json.Unmarshal(entry.M, &telArr); err2 != nil {
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
			if entry.T > lastSeenT {
				lastSeenT = entry.T
			}
			newCount++
		}
		if len(result.Logs) > 0 {
			lastBoundaryRaw = string(result.Logs[len(result.Logs)-1])
		}

		emit(SimEvent{
			Elapsed: time.Since(startT).Milliseconds(),
			Tag:     "FETCH",
			Cls:     "fe",
			Msg:     fmt.Sprintf("Page %d  %d entries (%d new, total: %d)", page, len(result.Logs), newCount, len(allRows)),
			Ty:      "info",
			Step:    "fetch:page",
			Data:    map[string]interface{}{"page": page, "count": len(result.Logs), "new": newCount, "total": len(allRows)},
		})

		// All entries in this page were duplicates of the boundary — we're done.
		if len(result.Logs) > 0 && newCount == 0 {
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
				return allRows, page, ctx.Err()
			case <-time.After(time.Duration(delay) * time.Millisecond):
			}
		}
	}

	// Stable sort by timestamp — preserves API-returned order for equal-timestamp entries.
	sort.SliceStable(allRows, func(i, j int) bool { return allRows[i].T < allRows[j].T })

	emit(SimEvent{
		Elapsed: time.Since(startT).Milliseconds(),
		Tag:     "FETCH",
		Cls:     "fe",
		Msg:     fmt.Sprintf("Done: %d OBD+GPS packets fetched", len(allRows)),
		Ty:      "ok",
		Step:    "fetch:done",
		Data:    map[string]interface{}{"packets": len(allRows), "pages": page},
	})
	return allRows, page, nil
}
