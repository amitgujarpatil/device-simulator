package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

const (
	apiBase   = "https://apis.intangles-aws-eu-north-1.eu.intangles.com"
	userToken = "CJyWAHleimwgcp7bLyvcfceqyXHrA4a78MtFON3D8YH50dONkEgt3fgdGY8WHH-R"
	srcImei   = "866308064390602"
	tgtImei   = "866308061027751"
)

var client = &http.Client{Timeout: 30 * time.Second}

func get(path string) (map[string]any, int, error) {
	req, _ := http.NewRequest("GET", apiBase+path, nil)
	req.Header.Set("Intangles-Client", "intangles_app")
	req.Header.Set("Intangles-User-Token", userToken)
	req.Header.Set("Intangles-Session-Type", "web")
	req.Header.Set("Accept", "application/json")
	t0 := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(t0).Round(time.Millisecond)
	if err != nil {
		fmt.Printf("  ✗ %s  (%s)\n", err, elapsed)
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("  ← %d  %s  (%d bytes)\n", resp.StatusCode, elapsed, len(body))
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		fmt.Printf("  ✗ JSON error: %v\nbody: %s\n", err, string(body[:min(500, len(body))]))
		return nil, resp.StatusCode, err
	}
	return result, resp.StatusCode, nil
}

func numToStr(v any) string {
	switch n := v.(type) {
	case float64:
		if n == 0 {
			return ""
		}
		return strconv.FormatInt(int64(n), 10)
	case string:
		return n
	}
	return ""
}

func lookupIMEI(imei string) (vehicleId, accId string, err error) {
	path := fmt.Sprintf(
		"/idevice/listV3?query=%s&psize=5&pnum=1&status=*&showall=true&lang=en",
		url.QueryEscape(imei),
	)
	fmt.Printf("→ GET /idevice/listV3?query=%s...\n", imei)
	res, _, err := get(path)
	if err != nil {
		return
	}
	result, _ := res["result"].(map[string]any)
	if result == nil {
		fmt.Printf("  raw response keys: %v\n", keys(res))
		err = fmt.Errorf("no 'result' in response")
		return
	}
	idevices, _ := result["idevices"].([]any)
	fmt.Printf("  idevices count: %d\n", len(idevices))
	for i, item := range idevices {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		vid := numToStr(m["vid"])
		aid := numToStr(m["account_id"])
		imeiField, _ := m["imei"].(string)
		fmt.Printf("  [%d] imei=%s  vid=%s  account_id=%s\n", i, imeiField, vid, aid)
		if vid != "" {
			vehicleId = vid
			accId = aid
			return
		}
	}
	err = fmt.Errorf("no device with vid found in list")
	return
}

func testTrips(vehicleId, accId string, fromMs, toMs int64) int {
	path := fmt.Sprintf(
		"/trip/%s/getLastTripsV2?start=%d&end=%d&psize=50&pnum=1&duration=0&acc_id=%s&lang=en",
		vehicleId, fromMs, toMs, accId,
	)
	fmt.Printf("→ GET /trip/%s/getLastTripsV2...\n", vehicleId)
	res, _, err := get(path)
	if err != nil {
		return -1
	}
	items, _ := res["trips"].([]any)
	fmt.Printf("  trips returned: %d\n", len(items))
	return len(items)
}

func testAlerts(vehicleId, accId string, fromMs, toMs int64) int {
	allTypes := "over_speed,speeding,idling,hard_brake,stoppage,freerun,unscheduled_driving,engine_over_running,geofence,over_acc,continuous_driving,slow_running,panick,device_disconnected,device_connected,dtc,fuel_chori,fuel_bhara,def_bhara,def_chori,def_low_level,fuel_low_level"
	path := fmt.Sprintf(
		"/alertlog/logsV2/%d/%d?psize=100&pnum=1&types=%s&vehicle_id=%s&acc_id=%s&lang=en&sort=timestamp%%20desc",
		fromMs, toMs, url.QueryEscape(allTypes), vehicleId, accId,
	)
	fmt.Printf("→ GET /alertlog/logsV2 vehicle=%s...\n", vehicleId)
	res, _, err := get(path)
	if err != nil {
		return -1
	}
	items, _ := res["logs"].([]any)
	fmt.Printf("  alerts returned: %d\n", len(items))
	return len(items)
}

func testDtcs(vehicleId, accId string, fromMs, toMs int64) int {
	path := fmt.Sprintf(
		"/dtc/vehicle/%s/history?pnum=1&psize=100&start_time=%d&end_time=%d&status=logged&unique_by_type=false&get_count=true&sort=timestamp%%20desc&acc_id=%s&lang=en",
		vehicleId, fromMs, toMs, accId,
	)
	fmt.Printf("→ GET /dtc/vehicle/%s/history...\n", vehicleId)
	res, _, err := get(path)
	if err != nil {
		return -1
	}
	items, _ := res["result"].([]any)
	fmt.Printf("  dtcs returned: %d\n", len(items))
	return len(items)
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func section(title string) {
	fmt.Printf("\n══ %s ══\n", title)
}

func main() {
	// Use same range as user's test + a wider range as fallback
	ranges := []struct {
		label      string
		fromMs, toMs int64
	}{
		{"user range (Sep 10–11 2026)", 1789065000000, 1789151400000},
		{"last 30 days",
			time.Now().Add(-30*24*time.Hour).UnixMilli(),
			time.Now().UnixMilli(),
		},
	}

	section("LOOKUP SRC " + srcImei)
	srcVid, srcAccId, err := lookupIMEI(srcImei)
	if err != nil {
		fmt.Printf("  ✗ src lookup failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  ✓ src vehicleId=%s  accId=%s\n", srcVid, srcAccId)

	section("LOOKUP TGT " + tgtImei)
	tgtVid, tgtAccId, err := lookupIMEI(tgtImei)
	if err != nil {
		fmt.Printf("  ✗ tgt lookup failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  ✓ tgt vehicleId=%s  accId=%s\n", tgtVid, tgtAccId)

	for _, r := range ranges {
		section(fmt.Sprintf("DATA FETCH — %s", r.label))
		fmt.Printf("  from=%d  to=%d\n\n", r.fromMs, r.toMs)

		fmt.Printf("[SRC] vehicleId=%s  accId=%s\n", srcVid, srcAccId)
		testTrips(srcVid, srcAccId, r.fromMs, r.toMs)
		testAlerts(srcVid, srcAccId, r.fromMs, r.toMs)
		testDtcs(srcVid, srcAccId, r.fromMs, r.toMs)

		fmt.Printf("\n[TGT] vehicleId=%s  accId=%s\n", tgtVid, tgtAccId)
		testTrips(tgtVid, tgtAccId, r.fromMs, r.toMs)
		testAlerts(tgtVid, tgtAccId, r.fromMs, r.toMs)
		testDtcs(tgtVid, tgtAccId, r.fromMs, r.toMs)
	}

	section("DONE")
}
