package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"device-simulator/pkg/apiclient"
	"device-simulator/pkg/mongoclient"
	"device-simulator/pkg/simulator"
	"device-simulator/pkg/utilities"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// logsSession holds state for an in-progress or completed logs fetch.
type logsSession struct {
	mu      sync.Mutex
	db      *sql.DB
	dbPath  string
	total   int
	pages   int
	fromMs  int64
	untilMs int64
	imei    string
	done    bool
	cancel  context.CancelFunc
}

// App is the main application struct.
type App struct {
	ctx      context.Context
	sim      *simulator.Simulator
	mqttSvc  *mqttService
	ac       *apiclient.DB
	logsMu   sync.Mutex
	logsSess *logsSession
}

// NewApp creates a new App application struct.
func NewApp() *App {
	return &App{sim: simulator.New(), mqttSvc: newMQTTService()}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.sim.SetContext(ctx)
}

// shutdown is called when the app terminates.
func (a *App) shutdown(_ context.Context) {
	a.sim.Stop()
	a.mqttSvc.disconnect()
}

// StartSimulation starts the simulation with the given config.
func (a *App) StartSimulation(cfg simulator.Config) error {
	return a.sim.Start(cfg, func(ev simulator.SimEvent) {
		runtime.EventsEmit(a.ctx, "simulation:event", ev)
	})
}

// StopSimulation stops the running simulation.
func (a *App) StopSimulation() {
	a.sim.Stop()
}

// UpdateMQTTPubIntervals updates the per-packet GPS and OBD delays in a running
// MQTT Direct simulation without restarting it (real-time slider changes).
func (a *App) UpdateMQTTPubIntervals(gpsMs, obdMs int) {
	a.sim.SetLiveIntervals(gpsMs, obdMs)
}

// UpdateNormalModeInterval updates the Phase 2 normal-mode per-packet delay in
// a running simulate-mode simulation without restarting it.
func (a *App) UpdateNormalModeInterval(ms int) {
	a.sim.SetNormalInterval(ms)
}

// UpdateNormalModeIntervals updates the Phase 2 GPS and OBD normal-mode delays separately.
func (a *App) UpdateNormalModeIntervals(gpsMs, obdMs int) {
	a.sim.SetNormalIntervals(gpsMs, obdMs)
}

// PauseSimulation pauses the running simulation.
func (a *App) PauseSimulation() {
	a.sim.Pause()
}

// ResumeSimulation resumes a paused simulation.
func (a *App) ResumeSimulation() {
	a.sim.Resume()
}

// TestMQTT tests an MQTT connection with the given region config.
func (a *App) TestMQTT(cfg simulator.RegionConfig) map[string]interface{} {
	ok, msg := simulator.TestMQTTConnection(cfg)
	return map[string]interface{}{"ok": ok, "message": msg}
}

// GenerateToken generates a random alphanumeric token.
func (a *App) GenerateToken() string {
	b := make([]byte, 20)
	rand.Read(b)
	_ = hex.EncodeToString(b)
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	result := make([]byte, 30)
	for i := range result {
		result[i] = chars[int(b[i%len(b)])%len(chars)]
	}
	return string(result)
}

// GetDefaultOutputDir returns the resolved default output path (~/ Documents/sim_output).
func (a *App) GetDefaultOutputDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Documents", "sim_output")
}

// SelectOutputDir opens a directory picker dialog.
func (a *App) SelectOutputDir() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Output Directory",
	})
}

// SaveFile opens a native save dialog and writes content to the chosen path.
func (a *App) SaveFile(content, defaultName string) error {
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		DefaultFilename: defaultName,
		Filters: []runtime.FileFilter{
			{DisplayName: "All Files", Pattern: "*"},
		},
	})
	if err != nil || path == "" {
		return err
	}
	// Preserve the expected extension — macOS NSSavePanel can strip it.
	if ext := filepath.Ext(defaultName); ext != "" && !strings.HasSuffix(strings.ToLower(path), strings.ToLower(ext)) {
		path += ext
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return err
	}
	// Remove macOS quarantine attribute so the file opens with the correct app.
	exec.Command("xattr", "-d", "com.apple.quarantine", path).Run() //nolint:errcheck
	return nil
}

// ClearTestState deletes the batch SQLite files and OBD accumulation DB that
// previous runs created for tgtImei inside outputDir so the next run starts fresh.
func (a *App) ClearTestState(outputDir, tgtImei string) error {
	if outputDir == "" {
		home, _ := os.UserHomeDir()
		outputDir = filepath.Join(home, "Documents", "sim_output")
	}
	patterns := []string{
		filepath.Join(outputDir, fmt.Sprintf("historic_batch_*_%s.db", tgtImei)),
		filepath.Join(outputDir, fmt.Sprintf("live_obd_%s.db", tgtImei)),
		filepath.Join(outputDir, fmt.Sprintf("sim_state_%s.json", tgtImei)),
		filepath.Join(outputDir, fmt.Sprintf("gps_l1_%s.json", tgtImei)),
		filepath.Join(outputDir, fmt.Sprintf("live_pkts_%s.json", tgtImei)),
	}
	var errs []string
	for _, pat := range patterns {
		matches, _ := filepath.Glob(pat)
		for _, f := range matches {
			if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
				errs = append(errs, filepath.Base(f)+": "+err.Error())
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("could not delete: %s", strings.Join(errs, "; "))
	}
	return nil
}

// SaveRunLogs persists all run log entries as a JSON array to outputDir.
// Called by the frontend when a simulation completes or is stopped.
func (a *App) SaveRunLogs(outputDir, runId string, logsJson string) error {
	if outputDir == "" {
		home, _ := os.UserHomeDir()
		outputDir = filepath.Join(home, "Documents", "sim_output")
	}
	if runId == "" {
		return nil
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return err
	}
	path := filepath.Join(outputDir, fmt.Sprintf("sim_logs_%s.json", runId))
	return os.WriteFile(path, []byte(logsJson), 0644)
}

// GetRunLogs returns one page of log entries for a run.
// page is 1-based; pageSize 0 defaults to 200.
func (a *App) GetRunLogs(outputDir, runId string, page, pageSize int) map[string]interface{} {
	result := map[string]interface{}{"entries": []interface{}{}, "total": 0, "page": 1, "pages": 0, "exists": false}
	if outputDir == "" {
		home, _ := os.UserHomeDir()
		outputDir = filepath.Join(home, "Documents", "sim_output")
	}
	if runId == "" {
		return result
	}
	path := filepath.Join(outputDir, fmt.Sprintf("sim_logs_%s.json", runId))
	data, err := os.ReadFile(path)
	if err != nil {
		return result
	}
	result["exists"] = true
	var entries []interface{}
	if err2 := json.Unmarshal(data, &entries); err2 != nil {
		return result
	}
	total := len(entries)
	if pageSize <= 0 {
		pageSize = 200
	}
	pages := (total + pageSize - 1) / pageSize
	if pages == 0 {
		pages = 1
	}
	if page < 1 {
		page = 1
	}
	start := (page - 1) * pageSize
	if start >= total {
		result["total"] = total
		result["pages"] = pages
		result["page"] = page
		return result
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	result["entries"] = entries[start:end]
	result["total"] = total
	result["pages"] = pages
	result["page"] = page
	return result
}

// ── API Client bridge ──────────────────────────────────────────────────────

// APIClientInit opens (or creates) the apiclient SQLite database in dir.
// Called by the frontend when the API Client page loads.
// If no workspaces exist, a "Default" workspace is created automatically.
func (a *App) APIClientInit(dir string) error {
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, "Documents", "sim_output")
	}
	if a.ac != nil {
		return nil // already open
	}
	db, err := apiclient.Open(dir)
	if err != nil {
		return err
	}
	a.ac = db
	// seed a default workspace if none exist
	ws, _ := a.ac.ListWorkspaces()
	if len(ws) == 0 {
		a.ac.SaveWorkspace(apiclient.Workspace{Name: "Default"})
	}
	return nil
}

func (a *App) APIClientSendRequest(p apiclient.SendPayload) apiclient.SendResponse {
	return apiclient.Execute(p)
}

// Workspaces

func (a *App) ACListWorkspaces() ([]apiclient.Workspace, error) {
	if a.ac == nil {
		return nil, fmt.Errorf("not initialised")
	}
	return a.ac.ListWorkspaces()
}

func (a *App) ACSaveWorkspace(w apiclient.Workspace) (apiclient.Workspace, error) {
	if a.ac == nil {
		return w, fmt.Errorf("not initialised")
	}
	return a.ac.SaveWorkspace(w)
}

func (a *App) ACDeleteWorkspace(id string) error {
	if a.ac == nil {
		return fmt.Errorf("not initialised")
	}
	return a.ac.DeleteWorkspace(id)
}

// Collections

func (a *App) ACListCollections(workspaceID string) ([]apiclient.Collection, error) {
	if a.ac == nil {
		return nil, fmt.Errorf("not initialised")
	}
	return a.ac.ListCollections(workspaceID)
}

func (a *App) ACSaveCollection(c apiclient.Collection) (apiclient.Collection, error) {
	if a.ac == nil {
		return c, fmt.Errorf("not initialised")
	}
	return a.ac.SaveCollection(c)
}

func (a *App) ACDeleteCollection(id string) error {
	if a.ac == nil {
		return fmt.Errorf("not initialised")
	}
	return a.ac.DeleteCollection(id)
}

// Requests

func (a *App) ACListRequests(workspaceID string) ([]apiclient.SavedRequest, error) {
	if a.ac == nil {
		return nil, fmt.Errorf("not initialised")
	}
	return a.ac.ListRequests(workspaceID)
}

func (a *App) ACSaveRequest(r apiclient.SavedRequest) (apiclient.SavedRequest, error) {
	if a.ac == nil {
		return r, fmt.Errorf("not initialised")
	}
	return a.ac.SaveRequest(r)
}

func (a *App) ACDeleteRequest(id string) error {
	if a.ac == nil {
		return fmt.Errorf("not initialised")
	}
	return a.ac.DeleteRequest(id)
}

// History

func (a *App) ACListHistory(workspaceID string) ([]apiclient.HistoryEntry, error) {
	if a.ac == nil {
		return nil, fmt.Errorf("not initialised")
	}
	return a.ac.ListHistory(workspaceID, 60)
}

func (a *App) ACSaveHistory(h apiclient.HistoryEntry) error {
	if a.ac == nil {
		return fmt.Errorf("not initialised")
	}
	return a.ac.SaveHistory(h)
}

func (a *App) ACClearHistory(workspaceID string) error {
	if a.ac == nil {
		return fmt.Errorf("not initialised")
	}
	return a.ac.ClearHistory(workspaceID)
}

// Environments

func (a *App) ACListEnvironments(workspaceID string) ([]apiclient.Environment, error) {
	if a.ac == nil {
		return nil, fmt.Errorf("not initialised")
	}
	return a.ac.ListEnvironments(workspaceID)
}

func (a *App) ACSaveEnvironment(e apiclient.Environment) (apiclient.Environment, error) {
	if a.ac == nil {
		return e, fmt.Errorf("not initialised")
	}
	return a.ac.SaveEnvironment(e)
}

func (a *App) ACSetActiveEnvironment(workspaceID, envID string) error {
	if a.ac == nil {
		return fmt.Errorf("not initialised")
	}
	return a.ac.SetActiveEnvironment(workspaceID, envID)
}

func (a *App) ACDeleteEnvironment(id string) error {
	if a.ac == nil {
		return fmt.Errorf("not initialised")
	}
	return a.ac.DeleteEnvironment(id)
}

func (a *App) ACListEnvVariables(envID string) ([]apiclient.EnvVariable, error) {
	if a.ac == nil {
		return nil, fmt.Errorf("not initialised")
	}
	return a.ac.ListEnvVariables(envID)
}

func (a *App) ACSaveEnvVariables(envID string, vars []apiclient.EnvVariable) error {
	if a.ac == nil {
		return fmt.Errorf("not initialised")
	}
	return a.ac.SaveEnvVariables(envID, vars)
}

// ACPickFile opens a native file dialog and returns {name, mime, base64, size}.
func (a *App) ACPickFile() (map[string]interface{}, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select File to Upload",
	})
	if err != nil || path == "" {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) > 10*1024*1024 {
		return nil, fmt.Errorf("file too large (max 10 MB)")
	}
	mime := http.DetectContentType(data)
	return map[string]interface{}{
		"name":   filepath.Base(path),
		"mime":   mime,
		"base64": base64.StdEncoding.EncodeToString(data),
		"size":   len(data),
	}, nil
}

// Export / Import

func (a *App) ACExportCollection(collectionID string) (string, error) {
	if a.ac == nil {
		return "", fmt.Errorf("not initialised")
	}
	return a.ac.ExportCollection(collectionID)
}

func (a *App) ACImportCollection(workspaceID, jsonStr string) error {
	if a.ac == nil {
		return fmt.Errorf("not initialised")
	}
	return a.ac.ImportCollection(workspaceID, jsonStr)
}

// ReadTextFile reads a file at path and returns its contents as a string.
func (a *App) ReadTextFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// GetResumeState inspects outputDir for tgtImei and returns full resume
// progress (batch files, packets, OBD rows, upload progress) from disk so
// the UI can show exactly where the previous run left off.
func (a *App) GetResumeState(outputDir, tgtImei string) map[string]interface{} {
	if outputDir == "" {
		home, _ := os.UserHomeDir()
		outputDir = filepath.Join(home, "Documents", "sim_output")
	} else if strings.HasPrefix(outputDir, "~/") {
		home, _ := os.UserHomeDir()
		outputDir = filepath.Join(home, outputDir[2:])
	}
	result := map[string]interface{}{
		"batchFiles":         0,
		"batchesUploaded":    0,
		"totalPackets":       0,
		"totalOBDPackets":    0,
		"obdRowsInDB":        0,
		"obdExists":          false,
		"phase1Complete":     false,
		"phase2OBDUploaded":  false,
		"liveStreamOffset":   0,
		"livePktsCached":     false,
		"outputDir":          outputDir,
	}
	if tgtImei == "" {
		return result
	}

	// Read run-state JSON written by the simulator
	type runStateJSON struct {
		BatchesUploaded   int  `json:"batchesUploaded"`
		TotalBatches      int  `json:"totalBatches"`
		TotalOBDPackets   int  `json:"totalOBDPackets"`
		Phase1Complete    bool `json:"phase1Complete"`
		Phase2OBDUploaded bool `json:"phase2ObdUploaded"`
		LiveStreamOffset  int  `json:"liveStreamOffset"`
		LiveGpsOffset     int  `json:"liveGpsOffset"`
		LiveObdOffset     int  `json:"liveObdOffset"`
		TotalPackets      int  `json:"totalPackets"`
		HistoricCount     int  `json:"historicCount"`
		LiveGpsCount      int  `json:"liveGpsCount"`
		LiveObdCount      int  `json:"liveObdCount"`
		FetchPages        int  `json:"fetchPages"`
		GpsL1Published    int  `json:"gpsL1Published"`
	}
	var st runStateJSON
	if b, err := os.ReadFile(filepath.Join(outputDir, fmt.Sprintf("sim_state_%s.json", tgtImei))); err == nil {
		json.Unmarshal(b, &st) //nolint:errcheck
	}
	result["batchesUploaded"]   = st.BatchesUploaded
	result["totalOBDPackets"]   = st.TotalOBDPackets
	result["phase1Complete"]    = st.Phase1Complete
	result["phase2OBDUploaded"] = st.Phase2OBDUploaded
	result["liveStreamOffset"]  = st.LiveStreamOffset
	result["liveGpsOffset"]     = st.LiveGpsOffset
	result["liveObdOffset"]     = st.LiveObdOffset
	result["historicCount"]     = st.HistoricCount
	result["liveGpsCount"]      = st.LiveGpsCount
	result["liveObdCount"]      = st.LiveObdCount
	result["fetchPages"]        = st.FetchPages
	result["gpsL1Published"]    = st.GpsL1Published
	// totalPackets from JSON takes priority; fall back to batch-file counting below
	if st.TotalPackets > 0 {
		result["totalPackets"] = st.TotalPackets
	}

	// Count batch files on disk
	pattern := filepath.Join(outputDir, fmt.Sprintf("historic_batch_*_%s.db", tgtImei))
	matches, _ := filepath.Glob(pattern)
	result["batchFiles"] = len(matches)

	// Sum packets across batch files
	totalPkts := 0
	for _, dbFile := range matches {
		db, err := sql.Open("sqlite", dbFile)
		if err != nil {
			continue
		}
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM oData").Scan(&count); err == nil {
			totalPkts += count
		}
		db.Close()
	}
	result["totalPackets"] = totalPkts

	// OBD DB
	obdPath := filepath.Join(outputDir, fmt.Sprintf("live_obd_%s.db", tgtImei))
	if _, err := os.Stat(obdPath); err == nil {
		result["obdExists"] = true
		db, err := sql.Open("sqlite", obdPath)
		if err == nil {
			var rows int
			if db.QueryRow("SELECT COUNT(*) FROM oData").Scan(&rows) == nil {
				result["obdRowsInDB"] = rows
			}
			db.Close()
		}
	}

	// Live-packets cache (for skip-fetch toggle)
	livePktsPath := filepath.Join(outputDir, fmt.Sprintf("live_pkts_%s.json", tgtImei))
	if _, err := os.Stat(livePktsPath); err == nil {
		result["livePktsCached"] = true
	}
	return result
}

// ── FetchLogsPage ──────────────────────────────────────────────────────────

// FetchLogsPage fetches one page of telemetry logs from the external API,
// bypassing browser CORS restrictions by making the request from Go.
func (a *App) FetchLogsPage(apiBase, imei, token string, fromMs, untilMs, psize, lastT int64) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/idevice/logsV2/%s?psize=%d&token=%s&from=%d&until=%d",
		apiBase, imei, psize, token, fromMs, untilMs)
	if lastT > 0 {
		url += fmt.Sprintf("&last_t=%d", lastT)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		msg := string(body)
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, msg)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return result, nil
}

// ExportFetchedData returns fetched packet data formatted for download.
// It first checks in-memory packets cached by the last fetch-only run (always
// unencrypted), then falls back to reading the batch SQLite files on disk (only
// works when encryption is disabled). kind is "all", "gps", or "obd";
// format is "json", "jsonl", or "csv".
func (a *App) ExportFetchedData(outputDir, tgtImei, kind, format string) (string, error) {
	// ── Primary: in-memory packets from the last fetch run ─────────────────
	var packets []map[string]interface{}
	if memPkts := a.sim.GetFetchedPackets(); len(memPkts) > 0 {
		for _, p := range memPkts {
			packets = append(packets, p.Packet)
		}
	}

	// ── Fallback: read SQLite batch files (only works unencrypted) ──────────
	if len(packets) == 0 {
		if outputDir == "" {
			home, _ := os.UserHomeDir()
			outputDir = filepath.Join(home, "Documents", "sim_output")
		} else if strings.HasPrefix(outputDir, "~/") {
			home, _ := os.UserHomeDir()
			outputDir = filepath.Join(home, outputDir[2:])
		}
		pattern := filepath.Join(outputDir, fmt.Sprintf("historic_batch_*_%s.db", tgtImei))
		matches, _ := filepath.Glob(pattern)
		sortBatchFiles(matches)
		for _, dbFile := range matches {
			db, err := sql.Open("sqlite", dbFile)
			if err != nil {
				continue
			}
			rows, err := db.Query("SELECT DATASTRING FROM oData ORDER BY DNO")
			if err != nil {
				db.Close()
				continue
			}
			for rows.Next() {
				var ds string
				if err := rows.Scan(&ds); err != nil {
					continue
				}
				var pkt map[string]interface{}
				if err := json.Unmarshal([]byte(ds), &pkt); err != nil {
					continue // skip encrypted / binary rows
				}
				packets = append(packets, pkt)
			}
			rows.Close()
			db.Close()
		}
	}

	if len(packets) == 0 {
		return "", fmt.Errorf("no exportable packets — run a fetch first, or disable encryption before fetching")
	}

	// Filter
	var out []map[string]interface{}
	for _, p := range packets {
		pt := exportPacketType(p)
		switch kind {
		case "gps":
			if pt == "gps" {
				out = append(out, p)
			}
		case "obd":
			if pt == "obd" {
				out = append(out, p)
			}
		default:
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return "", fmt.Errorf("no packets matched filter %q", kind)
	}

	// Format
	switch format {
	case "jsonl":
		var sb strings.Builder
		for _, pkt := range out {
			b, _ := json.Marshal(pkt)
			sb.Write(b)
			sb.WriteByte('\n')
		}
		return sb.String(), nil
	case "csv":
		return exportPacketsCSV(out), nil
	default: // json
		b, err := json.MarshalIndent(out, "", "  ")
		return string(b), err
	}
}

func exportPacketType(p map[string]interface{}) string {
	if _, ok := p["GA"]; ok { return "gps" }
	if _, ok := p["GD"]; ok { return "gps" }
	if _, ok := p["GT"]; ok { return "gps" }
	if _, ok := p["P"]; ok { return "obd" }
	if _, ok := p["DT_UDS3"]; ok { return "obd" }
	if _, ok := p["DT_UDS"]; ok { return "obd" }
	return "other"
}

func exportPacketsCSV(packets []map[string]interface{}) string {
	// Collect ordered unique keys (stable: _type first, then sorted remainder)
	keySet := map[string]struct{}{}
	for _, p := range packets {
		for k := range p {
			keySet[k] = struct{}{}
		}
	}
	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	// sort keys for deterministic output
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	keys = append([]string{"_type"}, keys...)

	var sb strings.Builder
	// header
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(csvQuote(k))
	}
	sb.WriteByte('\n')
	// rows
	for _, p := range packets {
		for i, k := range keys {
			if i > 0 {
				sb.WriteByte(',')
			}
			if k == "_type" {
				sb.WriteString(exportPacketType(p))
				continue
			}
			v, ok := p[k]
			if !ok {
				continue
			}
			switch tv := v.(type) {
			case string:
				sb.WriteString(csvQuote(tv))
			case float64:
				sb.WriteString(strconv.FormatFloat(tv, 'f', -1, 64))
			case bool:
				if tv {
					sb.WriteString("true")
				} else {
					sb.WriteString("false")
				}
			default:
				b, _ := json.Marshal(v)
				sb.WriteString(csvQuote(string(b)))
			}
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

func csvQuote(s string) string {
	if strings.ContainsAny(s, `,"` + "\n") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

func sortBatchFiles(files []string) {
	// simple insertion sort on the batch number embedded in the filename
	batchNum := func(name string) int {
		base := filepath.Base(name)
		// "historic_batch_N_<imei>.db"
		var n int
		fmt.Sscanf(base, "historic_batch_%d_", &n)
		return n
	}
	for i := 1; i < len(files); i++ {
		for j := i; j > 0 && batchNum(files[j]) < batchNum(files[j-1]); j-- {
			files[j], files[j-1] = files[j-1], files[j]
		}
	}
}

// GetSystemMetrics returns CPU, memory, and app footprint metrics.
func (a *App) GetSystemMetrics() map[string]interface{} {
	m := map[string]interface{}{}

	// ── Go runtime stats ────────────────────────────────────────────
	var ms goruntime.MemStats
	goruntime.ReadMemStats(&ms)
	m["appHeapAlloc"] = ms.HeapAlloc // bytes in use by live objects
	m["appHeapSys"] = ms.HeapSys     // heap reserved from OS
	m["appSys"] = ms.Sys             // total memory from OS
	m["goroutines"] = goruntime.NumGoroutine()

	// ── Process RSS via ps ──────────────────────────────────────────
	pid := os.Getpid()
	if out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "rss=").Output(); err == nil {
		if rss, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64); err == nil {
			m["appRSSBytes"] = rss * 1024
		}
	}
	// Process CPU% via ps (instantaneous snapshot)
	if out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "%cpu=").Output(); err == nil {
		if cpu, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64); err == nil {
			m["appCPUPct"] = cpu
		}
	}

	// ── System total RAM via sysctl ─────────────────────────────────
	if out, err := exec.Command("sysctl", "-n", "hw.memsize").Output(); err == nil {
		if total, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64); err == nil {
			m["sysTotalBytes"] = total
		}
	}

	// ── CPU core count ──────────────────────────────────────────────
	if out, err := exec.Command("sysctl", "-n", "hw.logicalcpu").Output(); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil {
			m["cpuCores"] = n
		}
	}

	// ── Load average ────────────────────────────────────────────────
	if out, err := exec.Command("sysctl", "-n", "vm.loadavg").Output(); err == nil {
		s := strings.Trim(strings.TrimSpace(string(out)), "{ }")
		parts := strings.Fields(s)
		if len(parts) >= 2 {
			if v, err := strconv.ParseFloat(parts[0], 64); err == nil {
				m["loadAvg1"] = v
			}
			if v, err := strconv.ParseFloat(parts[1], 64); err == nil {
				m["loadAvg5"] = v
			}
		}
	}

	// ── System used/free memory via vm_stat ─────────────────────────
	if out, err := exec.Command("vm_stat").Output(); err == nil {
		pageSize := int64(16384) // Apple Silicon default
		stats := map[string]int64{}
		for _, line := range strings.Split(string(out), "\n") {
			if strings.Contains(line, "page size of") {
				for i, p := range strings.Fields(line) {
					if p == "of" {
						if next := strings.Fields(line); len(next) > i+1 {
							if ps, err := strconv.ParseInt(next[i+1], 10, 64); err == nil {
								pageSize = ps
							}
						}
					}
				}
			}
			kv := strings.SplitN(line, ":", 2)
			if len(kv) == 2 {
				k := strings.TrimSpace(kv[0])
				v := strings.TrimRight(strings.TrimSpace(kv[1]), ".")
				if n, err := strconv.ParseInt(v, 10, 64); err == nil {
					stats[k] = n
				}
			}
		}
		wired := stats["Pages wired down"]
		active := stats["Pages active"] + stats["Pages occupied by compressor"]
		free := stats["Pages free"] + stats["Pages inactive"]
		m["sysUsedBytes"] = (wired + active) * pageSize
		m["sysFreeBytes"] = free * pageSize
	}

	return m
}

// ── Developer Utilities ───────────────────────────────────────────────────────

// UtilHash returns MD5, SHA1, SHA256 and SHA512 hex digests of text.
func (a *App) UtilHash(text string) map[string]string {
	return utilities.Hash(text)
}

// UtilHMAC computes HMAC-SHA256 or HMAC-SHA512 of text with key.
func (a *App) UtilHMAC(text, key, algo string) (string, error) {
	return utilities.HMAC(text, key, algo)
}

// UtilCompress compresses text with zlib or gzip and returns base64 + stats.
func (a *App) UtilCompress(text, algo string) (utilities.CompressInfo, error) {
	return utilities.CompressWithInfo(text, algo)
}

// UtilDecompress decompresses a base64-encoded zlib or gzip payload.
func (a *App) UtilDecompress(b64Input, algo string) (string, error) {
	return utilities.Decompress(b64Input, algo)
}

// UtilParseCert parses a PEM-encoded X.509 certificate.
func (a *App) UtilParseCert(pemText string) (utilities.CertInfo, error) {
	return utilities.ParseCert(pemText)
}

// UtilCertToFormats converts a PEM cert to inline string formats.
func (a *App) UtilCertToFormats(pemText string) (utilities.CertFormats, error) {
	return utilities.CertToFormats(pemText)
}

// UtilOpenTextFile opens a native file dialog and returns the file contents.
func (a *App) UtilOpenTextFile(title string) (string, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: title})
	if err != nil || path == "" {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// UtilYAMLToJSON converts YAML to indented JSON.
func (a *App) UtilYAMLToJSON(yamlStr string) (string, error) {
	return utilities.YAMLToJSON(yamlStr)
}

// UtilJSONToYAML converts JSON to YAML.
func (a *App) UtilJSONToYAML(jsonStr string) (string, error) {
	return utilities.JSONToYAML(jsonStr)
}

// ── MongoDB Studio bridge ─────────────────────────────────────────────────

// MCInit opens (or creates) the mongo client SQLite store in dir.
func (a *App) MCInit(dir string) error {
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, "Documents", "sim_output")
	}
	return mongoclient.InitStore(dir)
}

func (a *App) MCListSavedConnections() ([]mongoclient.Connection, error) {
	return mongoclient.ListSavedConnections()
}

func (a *App) MCSaveConnection(label, uri string) (mongoclient.Connection, error) {
	return mongoclient.SaveConnection(label, uri)
}

func (a *App) MCUpdateConnection(id, label, uri string) error {
	return mongoclient.UpdateConnection(id, label, uri)
}

func (a *App) MCDeleteConnection(id string) error {
	mongoclient.Disconnect(id)
	return mongoclient.DeleteConnection(id)
}

func (a *App) MCConnect(id string) error {
	conn, err := mongoclient.GetConnection(id)
	if err != nil {
		return fmt.Errorf("connection not found: %w", err)
	}
	return mongoclient.Connect(id, conn.URI)
}

func (a *App) MCTestConnection(uri string) error {
	tmpID := fmt.Sprintf("test_%d", time.Now().UnixMilli())
	if err := mongoclient.Connect(tmpID, uri); err != nil {
		return err
	}
	mongoclient.Disconnect(tmpID)
	return nil
}

func (a *App) MCDisconnect(id string) {
	mongoclient.Disconnect(id)
}

func (a *App) MCListDatabases(id string) ([]string, error) {
	return mongoclient.ListDatabases(id)
}

func (a *App) MCListCollections(id, db string) ([]mongoclient.CollectionMeta, error) {
	return mongoclient.ListCollections(id, db)
}

func (a *App) MCCollectionStats(id, db, coll string) (mongoclient.CollStats, error) {
	return mongoclient.GetCollStats(id, db, coll)
}

func (a *App) MCFind(id, db, coll, filter, sort, proj string, skip, limit int) (mongoclient.FindResult, error) {
	return mongoclient.Find(id, db, coll, filter, sort, proj, skip, limit)
}

func (a *App) MCInsertOne(id, db, coll, docJSON string) (string, error) {
	return mongoclient.InsertOne(id, db, coll, docJSON)
}

func (a *App) MCUpdateOne(id, db, coll, filter, update string) (int64, error) {
	return mongoclient.UpdateOne(id, db, coll, filter, update)
}

func (a *App) MCDeleteOne(id, db, coll, filter string) (int64, error) {
	return mongoclient.DeleteOne(id, db, coll, filter)
}

func (a *App) MCDeleteMany(id, db, coll, filter string) (int64, error) {
	return mongoclient.DeleteMany(id, db, coll, filter)
}

func (a *App) MCAggregate(id, db, coll, pipeline string, limit int) (mongoclient.FindResult, error) {
	return mongoclient.Aggregate(id, db, coll, pipeline, limit)
}

func (a *App) MCListIndexes(id, db, coll string) ([]mongoclient.Index, error) {
	return mongoclient.ListIndexes(id, db, coll)
}

func (a *App) MCCreateIndex(id, db, coll, keysJSON, optionsJSON string) (string, error) {
	return mongoclient.CreateIndex(id, db, coll, keysJSON, optionsJSON)
}

func (a *App) MCDropIndex(id, db, coll, indexName string) error {
	return mongoclient.DropIndex(id, db, coll, indexName)
}

func (a *App) MCSchemaAnalyze(id, db, coll, filter string, sampleSize int) ([]mongoclient.FieldStat, error) {
	return mongoclient.SchemaAnalyze(id, db, coll, filter, sampleSize)
}

func (a *App) MCExport(id, db, coll, filter, format string) error {
	content, err := mongoclient.Export(id, db, coll, filter, format)
	if err != nil {
		return err
	}
	ext := "." + format
	if format == "jsonl" {
		ext = ".jsonl"
	}
	return a.SaveFile(content, coll+ext)
}

func (a *App) MCImport(id, db, coll, format, filePath string, upsert bool) (mongoclient.ImportResult, error) {
	return mongoclient.Import(id, db, coll, format, filePath, upsert)
}

func (a *App) MCPickFile() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select file to import",
		Filters: []runtime.FileFilter{
			{DisplayName: "JSON / CSV", Pattern: "*.json;*.jsonl;*.csv"},
		},
	})
}

func (a *App) MCQueryHistory(connID, db, coll string) ([]mongoclient.QueryEntry, error) {
	return mongoclient.ListQueryHistory(connID, db, coll)
}

func (a *App) MCSaveQueryHistory(connID, db, coll, filter, sort, proj string) error {
	return mongoclient.SaveQueryHistory(connID, db, coll, filter, sort, proj)
}

func (a *App) MCClearQueryHistory(connID, db, coll string) error {
	return mongoclient.ClearQueryHistory(connID, db, coll)
}

func (a *App) MCCreateCollection(id, db, coll string) error {
	return mongoclient.CreateCollection(id, db, coll)
}

func (a *App) MCDropCollection(id, db, coll string) error {
	return mongoclient.DropCollection(id, db, coll)
}

func (a *App) MCRenameCollection(id, db, coll, newName string) error {
	return mongoclient.RenameCollection(id, db, coll, newName)
}

func (a *App) MCRunRaw(id, db, query string) (mongoclient.RawResult, error) {
	return mongoclient.RunRaw(id, db, query)
}

// ── Logs fetch session ──────────────────────────────────────────────────────

// LogsFetchStart fetches all API pages for the given IMEI + time window and
// stores raw entries in a SQLite DB. Progress is streamed via "logs:progress"
// events; "logs:done" fires when complete. maxEntries=0 means unlimited.
func (a *App) LogsFetchStart(apiBase, imei, token string, fromMs, untilMs, psize, maxEntries int64) error {
	if psize <= 0 {
		psize = 800
	}

	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "Documents", "sim_output")
	os.MkdirAll(dir, 0755)
	dbPath := filepath.Join(dir, fmt.Sprintf("logs_%s_%d_%d.db", imei, fromMs, untilMs))

	// Cancel any existing session.
	a.logsMu.Lock()
	if a.logsSess != nil && a.logsSess.cancel != nil {
		a.logsSess.cancel()
	}
	a.logsMu.Unlock()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS entries (
		id    INTEGER PRIMARY KEY AUTOINCREMENT,
		t     INTEGER NOT NULL,
		m_raw TEXT    NOT NULL
	); DELETE FROM entries;`); err != nil {
		db.Close()
		return fmt.Errorf("init db: %w", err)
	}

	ctx, cancel := context.WithCancel(a.ctx)
	sess := &logsSession{
		db: db, dbPath: dbPath,
		fromMs: fromMs, untilMs: untilMs, imei: imei,
		cancel: cancel,
	}

	a.logsMu.Lock()
	a.logsSess = sess
	a.logsMu.Unlock()

	go func() {
		defer cancel()
		client := &http.Client{Timeout: 30 * time.Second}
		var lastT int64
		var prevLekStr string
		var lastSeenT int64
		var lastBoundaryRaw string // raw JSON of last entry on previous page (the one API re-sends)
		page := 0
		total := 0

		for {
			select {
			case <-ctx.Done():
				runtime.EventsEmit(a.ctx, "logs:done", map[string]interface{}{
					"stopped": true, "total": total, "pages": page,
				})
				return
			default:
			}

			page++
			url := fmt.Sprintf("%s/idevice/logsV2/%s?psize=%d&token=%s&from=%d&until=%d",
				apiBase, imei, psize, token, fromMs, untilMs)
			if lastT > 0 {
				url += fmt.Sprintf("&last_t=%d", lastT)
			}

			var body []byte
			var fetchErr error
			for attempt := 1; attempt <= 3; attempt++ {
				req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
				if err != nil {
					fetchErr = err
					break
				}
				resp, err := client.Do(req)
				if err != nil {
					fetchErr = err
					if attempt < 3 {
						time.Sleep(2 * time.Second)
					}
					continue
				}
				body, err = io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil {
					fetchErr = err
					if attempt < 3 {
						time.Sleep(2 * time.Second)
					}
					continue
				}
				fetchErr = nil
				break
			}
			if fetchErr != nil {
				runtime.EventsEmit(a.ctx, "logs:error", fmt.Sprintf("page %d: %s", page, fetchErr))
				return
			}

			var result struct {
				Logs             []json.RawMessage      `json:"logs"`
				LastEvaluatedKey map[string]interface{} `json:"last_evaluated_key"`
			}
			if err := json.Unmarshal(body, &result); err != nil {
				runtime.EventsEmit(a.ctx, "logs:error", fmt.Sprintf("parse page %d: %s", page, err))
				return
			}

			// Insert new entries into SQLite.
			// Skip only the ONE exact entry the API re-sends (inclusive last_t).
			// Raw JSON equality means entries at the same timestamp that are
			// genuinely new pass through instead of being wrongly filtered.
			count := len(result.Logs)
			prevPageMaxT := lastSeenT
			prevBoundaryRaw := lastBoundaryRaw
			newCount := 0
			if count > 0 {
				tx, err := db.Begin()
				if err == nil {
					stmt, err2 := tx.Prepare("INSERT INTO entries(t, m_raw) VALUES(?,?)")
					if err2 == nil {
						boundarySkipped := false
						for _, raw := range result.Logs {
							rawStr := string(raw)
							var entry struct {
								T int64           `json:"t"`
								M json.RawMessage `json:"m"`
							}
							if json.Unmarshal(raw, &entry) != nil {
								continue
							}
							// Skip the exact boundary entry the API re-sends (once only).
							if !boundarySkipped && entry.T <= prevPageMaxT && rawStr == prevBoundaryRaw {
								boundarySkipped = true
								continue
							}
							var mStr string
							if err3 := json.Unmarshal(entry.M, &mStr); err3 != nil {
								mStr = string(entry.M)
							}
							stmt.Exec(entry.T, mStr)
							if entry.T > lastSeenT {
								lastSeenT = entry.T
							}
							total++
							newCount++
							if maxEntries > 0 && int64(total) >= maxEntries {
								break
							}
						}
						stmt.Close()
					}
					tx.Commit()
				}
				lastBoundaryRaw = string(result.Logs[len(result.Logs)-1])
			}

			sess.mu.Lock()
			sess.total = total
			sess.pages = page
			sess.mu.Unlock()

			runtime.EventsEmit(a.ctx, "logs:progress", map[string]interface{}{
				"page": page, "pageEntries": count, "newEntries": newCount, "total": total,
			})

			// Stop conditions (order matters):
			// 1. All entries in this page were duplicates — boundary loop, stop.
			if count > 0 && newCount == 0 {
				break
			}
			// 2. Hit user-defined entry limit.
			if maxEntries > 0 && int64(total) >= maxEntries {
				break
			}
			// 3. No continuation key.
			lekBytes, _ := json.Marshal(result.LastEvaluatedKey)
			lekStr := string(lekBytes)
			if result.LastEvaluatedKey == nil || lekStr == "null" || lekStr == prevLekStr {
				break
			}
			prevLekStr = lekStr

			// Advance cursor.
			if t, ok := result.LastEvaluatedKey["t"]; ok {
				switch v := t.(type) {
				case float64:
					lastT = int64(v)
				case json.Number:
					lastT, _ = v.Int64()
				}
			}

			time.Sleep(100 * time.Millisecond)
		}

		sess.mu.Lock()
		sess.done = true
		sess.mu.Unlock()

		runtime.EventsEmit(a.ctx, "logs:done", map[string]interface{}{
			"total": total, "pages": page, "dbPath": dbPath,
		})
	}()

	return nil
}

// LogsFetchStop cancels an in-progress fetch.
func (a *App) LogsFetchStop() {
	a.logsMu.Lock()
	defer a.logsMu.Unlock()
	if a.logsSess != nil && a.logsSess.cancel != nil {
		a.logsSess.cancel()
	}
}

// LogsGetPage returns a page of raw entries from the current session's SQLite DB.
// offset is zero-based entry index; limit is entries per page.
func (a *App) LogsGetPage(offset, limit int) (map[string]interface{}, error) {
	a.logsMu.Lock()
	sess := a.logsSess
	a.logsMu.Unlock()
	if sess == nil || sess.db == nil {
		return nil, fmt.Errorf("no logs session")
	}

	rows, err := sess.db.Query(
		"SELECT t, m_raw FROM entries ORDER BY t ASC, id ASC LIMIT ? OFFSET ?",
		limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type entry struct {
		T    int64  `json:"t"`
		MRaw string `json:"m_raw"`
	}
	var entries []entry
	for rows.Next() {
		var e entry
		if rows.Scan(&e.T, &e.MRaw) == nil {
			entries = append(entries, e)
		}
	}

	sess.mu.Lock()
	total := sess.total
	done := sess.done
	sess.mu.Unlock()

	return map[string]interface{}{
		"entries": entries,
		"total":   total,
		"done":    done,
		"offset":  offset,
		"limit":   limit,
	}, nil
}

// LogsGetMeta returns metadata for the current logs session.
func (a *App) LogsGetMeta() map[string]interface{} {
	a.logsMu.Lock()
	sess := a.logsSess
	a.logsMu.Unlock()
	if sess == nil {
		return map[string]interface{}{"ready": false}
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return map[string]interface{}{
		"ready":   true,
		"done":    sess.done,
		"total":   sess.total,
		"pages":   sess.pages,
		"fromMs":  sess.fromMs,
		"untilMs": sess.untilMs,
		"imei":    sess.imei,
		"dbPath":  sess.dbPath,
	}
}
