package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"device-simulator/pkg/simulator"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the main application struct.
type App struct {
	ctx     context.Context
	sim     *simulator.Simulator
	mqttSvc *mqttService
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
