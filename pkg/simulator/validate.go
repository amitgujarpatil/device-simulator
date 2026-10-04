package simulator

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

const (
	drivingAlertTypes = "over_speed,speeding,idling,hard_brake,stoppage,freerun,unscheduled_driving,engine_over_running,geofence,over_acc,continuous_driving,slow_running,panick"
	systemAlertTypes  = "device_disconnected,device_connected,dtc,fuel_chori,fuel_bhara,def_bhara,def_chori,def_low_level,fuel_low_level"
)

// ValidateConfig carries the credentials and time-range for a validation run.
type ValidateConfig struct {
	ApiBase   string `json:"apiBase"`
	UserToken string `json:"userToken"`
	AccId     string `json:"accId"`
	SrcImei   string `json:"srcImei"`
	TgtImei   string `json:"tgtImei"`
	FromMs    int64  `json:"fromMs"`
	ToMs      int64  `json:"toMs"`
}

// VehicleValidation holds the fetched data for a single vehicle.
type VehicleValidation struct {
	VehicleId        string           `json:"vehicleId"`
	Imei             string           `json:"imei"`
	Trips            []map[string]any `json:"trips"`
	TripCount        int              `json:"tripCount"`
	Alerts           []map[string]any `json:"alerts"`
	AlertCount       int              `json:"alertCount"`
	AlertsByType     map[string]int   `json:"alertsByType"`
	DtcsActive       []map[string]any `json:"dtcsActive"`
	DtcActiveCount   int              `json:"dtcActiveCount"`
	DtcsInactive     []map[string]any `json:"dtcsInactive"`
	DtcInactiveCount int              `json:"dtcInactiveCount"`
	Errors           []string         `json:"errors,omitempty"`
}

// ValidateResult is the final comparison result returned to the frontend.
type ValidateResult struct {
	Src       VehicleValidation `json:"src"`
	Tgt       VehicleValidation `json:"tgt"`
	CheckedAt string            `json:"checkedAt"`
	Error     string            `json:"error,omitempty"`
	Logs      []string          `json:"logs,omitempty"`
}

// valLogger collects timestamped log lines during a validation run.
type valLogger struct {
	mu   sync.Mutex
	msgs []string
}

func (l *valLogger) add(format string, args ...any) {
	ts := time.Now().Format("15:04:05.000")
	line := ts + "  " + fmt.Sprintf(format, args...)
	l.mu.Lock()
	l.msgs = append(l.msgs, line)
	l.mu.Unlock()
}

func (l *valLogger) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, len(l.msgs))
	copy(out, l.msgs)
	return out
}

func valAPIGet(apiBase, path, userToken string, lg *valLogger) (map[string]any, error) {
	fullURL := apiBase + path
	lg.add("→ GET %s", fullURL)
	t0 := time.Now()

	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		lg.add("  request build error: %v", err)
		return nil, err
	}
	req.Header.Set("Intangles-Client", "intangles_app")
	req.Header.Set("Intangles-User-Token", userToken)
	req.Header.Set("Intangles-Session-Type", "web")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	elapsed := time.Since(t0).Round(time.Millisecond)
	if err != nil {
		lg.add("  ✗ error after %s: %v", elapsed, err)
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	lg.add("  ← %d  %s  (%d bytes)", resp.StatusCode, elapsed, len(body))

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		snippet := string(body)
		if len(snippet) > 200 {
			snippet = snippet[:200] + "…"
		}
		lg.add("  ✗ JSON decode error: %v  body: %s", err, snippet)
		return nil, fmt.Errorf("decode error (status %d): %w", resp.StatusCode, err)
	}
	return result, nil
}

// DeviceLookup holds the resolved vehicleId and accId for a device IMEI.
type DeviceLookup struct {
	VehicleId string `json:"vehicleId"`
	AccId     string `json:"accId"`
}

// numToStr converts a JSON number (float64 from Go's json.Unmarshal) to a decimal string.
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

func lookupDevice(apiBase, userToken, imei string, lg *valLogger) (DeviceLookup, error) {
	lg.add("[lookup] imei=%s", imei)
	// listV3 searches by IMEI query param and is reliable across all device types.
	// vid and account_id come back as JSON numbers (float64) — numToStr handles the conversion.
	path := fmt.Sprintf(
		"/idevice/listV3?query=%s&psize=5&pnum=1&status=*&showall=true&lang=en",
		url.QueryEscape(imei),
	)
	res, err := valAPIGet(apiBase, path, userToken, lg)
	if err != nil {
		return DeviceLookup{}, err
	}

	result, _ := res["result"].(map[string]any)
	if result == nil {
		if st, ok := res["status"].(map[string]any); ok {
			lg.add("  API status: %v", st)
		}
		if msg, ok := res["message"]; ok {
			lg.add("  API message: %v", msg)
		}
		return DeviceLookup{}, fmt.Errorf("device not found for imei %s (no result in response)", imei)
	}

	idevices, _ := result["idevices"].([]any)
	for _, item := range idevices {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		vid := numToStr(m["vid"])
		aid := numToStr(m["account_id"])
		if vid != "" {
			lg.add("  vehicleId=%s  accId=%s", vid, aid)
			return DeviceLookup{VehicleId: vid, AccId: aid}, nil
		}
	}

	return DeviceLookup{}, fmt.Errorf("device not found for imei %s (no vid in idevices list)", imei)
}

// LookupDevice is the public API (used from app.go bridge).
func LookupDevice(apiBase, userToken, imei string) (DeviceLookup, error) {
	lg := &valLogger{}
	return lookupDevice(apiBase, userToken, imei, lg)
}

// LookupVehicleId is a backward-compat wrapper that returns only the vehicleId.
func LookupVehicleId(apiBase, userToken, imei string) (string, error) {
	d, err := LookupDevice(apiBase, userToken, imei)
	return d.VehicleId, err
}

func fetchAllTrips(apiBase, userToken, accId, vehicleId string, fromMs, toMs int64, lg *valLogger) ([]map[string]any, error) {
	lg.add("[trips] vehicleId=%s", vehicleId)
	var all []map[string]any
	psize := 50
	for pnum := 1; ; pnum++ {
		path := fmt.Sprintf(
			"/trip/%s/getLastTripsV2?start=%d&end=%d&psize=%d&pnum=%d&duration=0&acc_id=%s&lang=en",
			vehicleId, fromMs, toMs, psize, pnum, accId,
		)
		res, err := valAPIGet(apiBase, path, userToken, lg)
		if err != nil {
			return all, err
		}
		items, _ := res["trips"].([]any)
		for _, it := range items {
			if m, ok := it.(map[string]any); ok {
				all = append(all, m)
			}
		}
		lg.add("  trips page %d → %d items (total so far: %d)", pnum, len(items), len(all))
		if len(items) < psize {
			break
		}
	}
	lg.add("  trips done: %d total", len(all))
	return all, nil
}

func fetchAllAlerts(apiBase, userToken, accId, vehicleId string, fromMs, toMs int64, lg *valLogger) ([]map[string]any, map[string]int, error) {
	lg.add("[alerts] vehicleId=%s", vehicleId)
	allTypes := drivingAlertTypes + "," + systemAlertTypes
	var all []map[string]any
	byType := map[string]int{}
	psize := 100
	for pnum := 1; ; pnum++ {
		path := fmt.Sprintf(
			"/alertlog/logsV2/%d/%d?psize=%d&pnum=%d&types=%s&vehicles=%s&acc_id=%s&lang=en&sort=timestamp%%20desc&no_total=true",
			fromMs, toMs, psize, pnum, url.QueryEscape(allTypes), vehicleId, accId,
		)
		res, err := valAPIGet(apiBase, path, userToken, lg)
		if err != nil {
			return all, byType, err
		}
		items, _ := res["logs"].([]any)
		for _, it := range items {
			if m, ok := it.(map[string]any); ok {
				all = append(all, m)
				if t, ok := m["alert_type"].(string); ok {
					byType[t]++
				}
			}
		}
		lg.add("  alerts page %d → %d items (total so far: %d)", pnum, len(items), len(all))
		if len(items) < psize {
			break
		}
	}
	lg.add("  alerts done: %d total", len(all))
	return all, byType, nil
}

func fetchAllDtcs(apiBase, userToken, accId, vehicleId, dtcSt string, fromMs, toMs int64, lg *valLogger) ([]map[string]any, error) {
	lg.add("[dtcs:%s] vehicleId=%s", dtcSt, vehicleId)
	var all []map[string]any
	psize := 100
	for pnum := 1; ; pnum++ {
		path := fmt.Sprintf(
			"/dtc/vehicle/%s/history?pnum=%d&psize=%d&start_time=%d&end_time=%d&status=%s&unique_by_type=false&get_count=true&sort=timestamp%%20desc&acc_id=%s&lang=en",
			vehicleId, pnum, psize, fromMs, toMs, dtcSt, accId,
		)
		res, err := valAPIGet(apiBase, path, userToken, lg)
		if err != nil {
			return all, err
		}
		items, _ := res["result"].([]any)
		for _, it := range items {
			if m, ok := it.(map[string]any); ok {
				all = append(all, m)
			}
		}
		lg.add("  dtcs[%s] page %d → %d items (total so far: %d)", dtcSt, pnum, len(items), len(all))
		if len(items) < psize {
			break
		}
	}
	lg.add("  dtcs[%s] done: %d total", dtcSt, len(all))
	return all, nil
}

func validateVehicle(apiBase, userToken, accId, vehicleId, imei string, fromMs, toMs int64, lg *valLogger) VehicleValidation {
	v := VehicleValidation{VehicleId: vehicleId, Imei: imei, AlertsByType: map[string]int{}}

	trips, err := fetchAllTrips(apiBase, userToken, accId, vehicleId, fromMs, toMs, lg)
	if err != nil {
		lg.add("  ✗ trips error: %v", err)
		v.Errors = append(v.Errors, "trips: "+err.Error())
	} else {
		v.Trips = trips
		v.TripCount = len(trips)
	}

	alerts, byType, err := fetchAllAlerts(apiBase, userToken, accId, vehicleId, fromMs, toMs, lg)
	if err != nil {
		lg.add("  ✗ alerts error: %v", err)
		v.Errors = append(v.Errors, "alerts: "+err.Error())
	} else {
		v.Alerts = alerts
		v.AlertCount = len(alerts)
		v.AlertsByType = byType
	}

	dtcsActive, err := fetchAllDtcs(apiBase, userToken, accId, vehicleId, "active", fromMs, toMs, lg)
	if err != nil {
		lg.add("  ✗ dtcs(active) error: %v", err)
		v.Errors = append(v.Errors, "dtcs(active): "+err.Error())
	} else {
		v.DtcsActive = dtcsActive
		v.DtcActiveCount = len(dtcsActive)
	}

	dtcsInactive, err := fetchAllDtcs(apiBase, userToken, accId, vehicleId, "inactive", fromMs, toMs, lg)
	if err != nil {
		lg.add("  ✗ dtcs(inactive) error: %v", err)
		v.Errors = append(v.Errors, "dtcs(inactive): "+err.Error())
	} else {
		v.DtcsInactive = dtcsInactive
		v.DtcInactiveCount = len(dtcsInactive)
	}

	return v
}

// RunValidation always looks up both IMEIs fresh, then fetches trips/alerts/DTCs
// for each device using its own accId. Nothing is cached.
func RunValidation(cfg ValidateConfig) ValidateResult {
	lg := &valLogger{}
	result := ValidateResult{CheckedAt: time.Now().UTC().Format(time.RFC3339)}

	lg.add("RunValidation start  src=%s  tgt=%s  from=%d  to=%d", cfg.SrcImei, cfg.TgtImei, cfg.FromMs, cfg.ToMs)

	type deviceResolve struct {
		side string
		imei string
		d    DeviceLookup
		err  error
	}

	ch := make(chan deviceResolve, 2)

	go func() {
		d, err := lookupDevice(cfg.ApiBase, cfg.UserToken, cfg.SrcImei, lg)
		ch <- deviceResolve{"src", cfg.SrcImei, d, err}
	}()
	go func() {
		d, err := lookupDevice(cfg.ApiBase, cfg.UserToken, cfg.TgtImei, lg)
		ch <- deviceResolve{"tgt", cfg.TgtImei, d, err}
	}()

	var srcLookup, tgtLookup DeviceLookup
	for i := 0; i < 2; i++ {
		lr := <-ch
		if lr.err != nil {
			result.Error = "lookup " + lr.side + " (" + lr.imei + "): " + lr.err.Error()
			result.Logs = lg.all()
			return result
		}
		if lr.side == "src" {
			srcLookup = lr.d
		} else {
			tgtLookup = lr.d
		}
	}

	lg.add("lookup done  src vehicleId=%s accId=%s  tgt vehicleId=%s accId=%s",
		srcLookup.VehicleId, srcLookup.AccId, tgtLookup.VehicleId, tgtLookup.AccId)

	if srcLookup.VehicleId == "" {
		result.Error = "vehicle not found for src imei " + cfg.SrcImei
		result.Logs = lg.all()
		return result
	}
	if tgtLookup.VehicleId == "" {
		result.Error = "vehicle not found for tgt imei " + cfg.TgtImei
		result.Logs = lg.all()
		return result
	}

	// Each device has its own vehicleId and accId — fetch independently
	var srcVal, tgtVal VehicleValidation
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		lg.add("[src] fetching data  vehicleId=%s  accId=%s", srcLookup.VehicleId, srcLookup.AccId)
		srcVal = validateVehicle(cfg.ApiBase, cfg.UserToken, srcLookup.AccId, srcLookup.VehicleId, cfg.SrcImei, cfg.FromMs, cfg.ToMs, lg)
	}()
	go func() {
		defer wg.Done()
		lg.add("[tgt] fetching data  vehicleId=%s  accId=%s", tgtLookup.VehicleId, tgtLookup.AccId)
		tgtVal = validateVehicle(cfg.ApiBase, cfg.UserToken, tgtLookup.AccId, tgtLookup.VehicleId, cfg.TgtImei, cfg.FromMs, cfg.ToMs, lg)
	}()
	wg.Wait()

	lg.add("RunValidation done  src trips=%d alerts=%d dtc(a=%d i=%d)  tgt trips=%d alerts=%d dtc(a=%d i=%d)",
		srcVal.TripCount, srcVal.AlertCount, srcVal.DtcActiveCount, srcVal.DtcInactiveCount,
		tgtVal.TripCount, tgtVal.AlertCount, tgtVal.DtcActiveCount, tgtVal.DtcInactiveCount)

	result.Src = srcVal
	result.Tgt = tgtVal
	result.Logs = lg.all()
	return result
}
