package simulator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// EventEmitter is a function that emits a SimEvent to the frontend.
type EventEmitter func(SimEvent)

// simRunState records Phase 1 upload progress so a stopped run can resume
// exactly where it left off without re-uploading or re-accumulating data.
type simRunState struct {
	BatchesUploaded   int  `json:"batchesUploaded"`
	TotalBatches      int  `json:"totalBatches"`
	TotalOBDPackets   int  `json:"totalOBDPackets"`
	Phase1Complete    bool `json:"phase1Complete"`
	Phase2OBDUploaded bool `json:"phase2ObdUploaded"`
	LiveStreamOffset  int  `json:"liveStreamOffset"` // legacy
	LiveGpsOffset     int  `json:"liveGpsOffset"`
	LiveObdOffset     int  `json:"liveObdOffset"`
	// Stats for UI restore after stop/restart
	TotalPackets   int `json:"totalPackets"`
	HistoricCount  int `json:"historicCount"`
	LiveGpsCount   int `json:"liveGpsCount"`
	LiveObdCount   int `json:"liveObdCount"`
	FetchPages     int `json:"fetchPages"`
	GpsL1Published int `json:"gpsL1Published"`
}

func runStateFile(outDir, tgtIMEI string) string {
	return filepath.Join(outDir, fmt.Sprintf("sim_state_%s.json", tgtIMEI))
}

func loadRunState(outDir, tgtIMEI string) simRunState {
	b, err := os.ReadFile(runStateFile(outDir, tgtIMEI))
	if err != nil {
		return simRunState{}
	}
	var st simRunState
	json.Unmarshal(b, &st) //nolint:errcheck
	return st
}

func saveRunState(outDir, tgtIMEI string, st simRunState) {
	b, _ := json.Marshal(st)
	os.WriteFile(runStateFile(outDir, tgtIMEI), b, 0644) //nolint:errcheck
}

func gpsL1StateFile(outDir, tgtIMEI string) string {
	return filepath.Join(outDir, fmt.Sprintf("gps_l1_%s.json", tgtIMEI))
}

func saveGpsL1Index(path string, idx int) {
	b, _ := json.Marshal(struct {
		Index int `json:"index"`
	}{Index: idx})
	os.WriteFile(path, b, 0644) //nolint:errcheck
}

func loadGpsL1Index(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var st struct {
		Index int `json:"index"`
	}
	json.Unmarshal(b, &st) //nolint:errcheck
	return st.Index
}

// Simulator manages the simulation lifecycle.
type Simulator struct {
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	paused   bool
	startT   time.Time

	// Live-updatable intervals (updated via bridge while sim is running).
	liveMu          sync.RWMutex
	liveGpsMs       int
	liveObdMs       int
	liveNormalMs    int
	liveNormalGpsMs int
	liveNormalObdMs int

	// Raw packets stored after a fetch-only run so the app can export them
	// without touching the (possibly encrypted) SQLite batch files.
	fetchMu       sync.RWMutex
	fetchedPackets []Packet
}

// StoreFetchedPackets saves the raw (pre-encryption) packets for later export.
func (s *Simulator) StoreFetchedPackets(pkts []Packet) {
	s.fetchMu.Lock()
	s.fetchedPackets = pkts
	s.fetchMu.Unlock()
}

// GetFetchedPackets returns the last set of fetched packets (nil if not set).
func (s *Simulator) GetFetchedPackets() []Packet {
	s.fetchMu.RLock()
	defer s.fetchMu.RUnlock()
	return s.fetchedPackets
}

func (s *Simulator) SetLiveIntervals(gpsMs, obdMs int) {
	s.liveMu.Lock()
	s.liveGpsMs = gpsMs
	s.liveObdMs = obdMs
	s.liveMu.Unlock()
}

func (s *Simulator) getLiveIntervals() (int, int) {
	s.liveMu.RLock()
	defer s.liveMu.RUnlock()
	return s.liveGpsMs, s.liveObdMs
}

func (s *Simulator) SetNormalInterval(ms int) {
	s.SetNormalIntervals(ms, ms)
}

func (s *Simulator) SetNormalIntervals(gpsMs, obdMs int) {
	s.liveMu.Lock()
	s.liveNormalGpsMs = gpsMs
	s.liveNormalObdMs = obdMs
	// keep legacy field in sync for any callers that still read it
	if gpsMs > 0 {
		s.liveNormalMs = gpsMs
	} else {
		s.liveNormalMs = obdMs
	}
	s.liveMu.Unlock()
}

func (s *Simulator) getLiveNormalInterval() time.Duration {
	return s.getLiveNormalGpsInterval()
}

func (s *Simulator) getLiveNormalGpsInterval() time.Duration {
	s.liveMu.RLock()
	ms := s.liveNormalGpsMs
	s.liveMu.RUnlock()
	if ms <= 0 {
		return 60 * time.Second
	}
	return time.Duration(ms) * time.Millisecond
}

func (s *Simulator) getLiveNormalObdInterval() time.Duration {
	s.liveMu.RLock()
	ms := s.liveNormalObdMs
	s.liveMu.RUnlock()
	if ms <= 0 {
		return 60 * time.Second
	}
	return time.Duration(ms) * time.Millisecond
}

// New creates a new Simulator instance.
func New() *Simulator {
	return &Simulator{}
}

// SetContext sets the parent context (from Wails startup).
func (s *Simulator) SetContext(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ctx = ctx
}

// IsRunning returns true if a simulation is currently running.
func (s *Simulator) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancel != nil
}

// Start launches a simulation goroutine and returns immediately.
func (s *Simulator) Start(cfg Config, emit EventEmitter) error {
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return fmt.Errorf("simulation already running")
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.cancel = cancel
	s.startT = time.Now()
	s.paused = false
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			s.cancel = nil
			s.mu.Unlock()
		}()
		if err := runSimulation(ctx, cfg, emit, s.startT, s); err != nil {
			if ctx.Err() == nil {
				emit(SimEvent{
					Elapsed: time.Since(s.startT).Milliseconds(),
					Tag:     "ERROR",
					Cls:     "er",
					Msg:     err.Error(),
					Ty:      "err",
					Step:    "error",
				})
			}
		}
	}()
	return nil
}

// Stop cancels the running simulation.
func (s *Simulator) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}

// Pause pauses emission of new events (checked at checkpoints).
func (s *Simulator) Pause() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paused = true
}

// Resume resumes the simulation.
func (s *Simulator) Resume() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paused = false
}

// checkPause blocks if the simulator is paused; returns error if context cancelled.
func (s *Simulator) checkPause(ctx context.Context) error {
	for {
		s.mu.Lock()
		p := s.paused
		s.mu.Unlock()
		if !p {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// waitInterruptible sleeps for d but wakes every 100ms to check for context
// cancellation and pause, so Stop/Pause takes effect promptly even for long
// per-packet intervals.  Returns ctx.Err() if cancelled/paused-then-cancelled,
// nil when the full duration has elapsed.
func (s *Simulator) waitInterruptible(ctx context.Context, d time.Duration) error {
	deadline := time.Now().Add(d)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil
		}
		tick := 100 * time.Millisecond
		if remaining < tick {
			tick = remaining
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(tick):
		}
		if err := s.checkPause(ctx); err != nil {
			return err
		}
	}
}

// waitInterruptibleWithStop is like waitInterruptible but also exits when
// stopCh is closed (used for GPS L1 which has its own stop channel).
func (s *Simulator) waitInterruptibleWithStop(ctx context.Context, d time.Duration, stopCh <-chan struct{}) error {
	deadline := time.Now().Add(d)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil
		}
		tick := 100 * time.Millisecond
		if remaining < tick {
			tick = remaining
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-stopCh:
			return ctx.Err() // treat stop-channel close as cancellation
		case <-time.After(tick):
		}
		if err := s.checkPause(ctx); err != nil {
			return err
		}
	}
}

// runSimulation is the main simulation orchestrator.
func runSimulation(ctx context.Context, cfg Config, emit EventEmitter, startT time.Time, s *Simulator) error {
	elap := func() int64 { return time.Since(startT).Milliseconds() }

	// ── Resolve public key ───────────────────────────────────────────────────
	publicKeyPEM := embeddedRSAPublicKey
	if cfg.RSAKey != "" && cfg.RSAKey != "embedded" {
		data, err := os.ReadFile(cfg.RSAKey)
		if err == nil {
			publicKeyPEM = string(data)
		}
	}

	// ── Resolve output dir ───────────────────────────────────────────────────
	outDir := cfg.OutputDir
	home, _ := os.UserHomeDir()
	if outDir == "" {
		outDir = filepath.Join(home, "Documents", "sim_output")
	} else if strings.HasPrefix(outDir, "~/") {
		outDir = filepath.Join(home, outDir[2:])
	} else if !filepath.IsAbs(outDir) {
		// Relative paths (e.g. "./sim_output") resolve from ~/Documents so the
		// .app bundle's read-only working directory is never used.
		outDir = filepath.Join(home, "Documents", outDir)
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	// Load run state early — needed for SkipFetch and resume decisions.
	runState := loadRunState(outDir, cfg.TgtIMEI)

	// ── Live-packets cache path (used by SkipFetch) ──────────────────────────
	livePktsPath := filepath.Join(outDir, fmt.Sprintf("live_pkts_%s.json", cfg.TgtIMEI))

	var allPackets []Packet
	var historicRows []map[string]interface{}
	var livePkts []livePacket
	var liveGpsPackets []map[string]interface{}
	var liveObdPackets []map[string]interface{}
	var batchFiles []string

	// When Phase 1 is already complete, always use the cached live-packets file so
	// Phase 2 resume offsets (LiveGpsOffset/LiveObdOffset) remain valid — they
	// reference positions in the ORIGINAL fetch order.  Re-fetching fresh data
	// would produce a different packet list, making saved offsets stale.
	// Users who want a true fresh run must call ClearTestState first.
	skipFetchDone := false
	if cfg.Mode == "simulate" && runState.Phase1Complete {
		if data, err2 := os.ReadFile(livePktsPath); err2 == nil {
			var cached []livePacket
			if json.Unmarshal(data, &cached) == nil && len(cached) > 0 {
				livePkts = cached
				skipFetchDone = true
				emit(SimEvent{
					Elapsed: elap(), Tag: "FETCH", Cls: "fe",
					Msg:  fmt.Sprintf("Phase 1 complete — resuming with %d cached live packets", len(livePkts)),
					Ty:   "ok", Step: "fetch:done",
					Data: map[string]interface{}{"packets": len(livePkts), "cached": true},
				})
			}
		}
		if !skipFetchDone {
			emit(SimEvent{
				Elapsed: elap(), Tag: "FETCH", Cls: "warn",
				Msg: "Phase 1 complete but live-packets cache missing — re-fetching from API", Ty: "warn",
			})
		}
	}

	if !skipFetchDone {
		// ── Step 1: Fetch ──────────────────────────────────────────────────────
		if err := s.checkPause(ctx); err != nil {
			return nil
		}
		var fetchErr error
		var fetchPages int
		allPackets, fetchPages, fetchErr = fetchTelemetry(ctx, cfg, emit, startT)
		if fetchErr != nil {
			return fmt.Errorf("fetch: %w", fetchErr)
		}
		runState.TotalPackets = len(allPackets)
		runState.FetchPages = fetchPages
		// Zero out split-derived fields so stale values from a previous run on
		// the same IMEI don't persist in the state file between this fetch-save
		// and the split-save that follows. They will be set correctly once the
		// split completes.
		runState.HistoricCount = 0
		runState.LiveGpsCount = 0
		runState.LiveObdCount = 0
		saveRunState(outDir, cfg.TgtIMEI, runState)
		if ctx.Err() != nil {
			return nil
		}

		// ── Sort validation ───────────────────────────────────────────────────
		// fetchTelemetry already sorts, but guard defensively before any processing.
		sorted := true
		for i := 1; i < len(allPackets); i++ {
			if allPackets[i].T < allPackets[i-1].T {
				sorted = false
				break
			}
		}
		if !sorted {
			sort.SliceStable(allPackets, func(i, j int) bool { return allPackets[i].T < allPackets[j].T })
			emit(SimEvent{
				Elapsed: elap(), Tag: "SORT", Cls: "er",
				Msg: fmt.Sprintf("⚠ Packets were out of order — re-sorted (%d packets)", len(allPackets)),
				Ty: "warn", Step: "sort:done",
			})
		} else if len(allPackets) > 0 {
			first, last := allPackets[0].T, allPackets[len(allPackets)-1].T
			emit(SimEvent{
				Elapsed: elap(), Tag: "SORT", Cls: "fe",
				Msg: fmt.Sprintf("Order validated ✓  %d packets  t=[%d … %d]", len(allPackets), first, last),
				Ty: "info", Step: "sort:done",
				Data: map[string]interface{}{"packets": len(allPackets), "firstT": first, "lastT": last},
			})
		}

		// ── Step 2: Split ─────────────────────────────────────────────────────
		emit(SimEvent{
			Elapsed: elap(),
			Tag:     "SPLIT",
			Cls:     "sp",
			Msg:     "Partitioning data into historic and live sets…",
			Ty:      "info",
			Step:    "split:start",
		})

		historyEndMS := cfg.HistoryEndMS
		if historyEndMS == 0 {
			defaultHours := cfg.HistEndDefaultHours
			if defaultHours <= 0 {
				defaultHours = 12
			}
			historyEndMS = cfg.FromMS + int64(defaultHours)*3600*1000
			// Cap the auto-computed default so it never swallows the entire fetch window.
			// If the time range is shorter than the default window (common for tracker/GPS-only
			// devices), historyEndMS would exceed untilMs and leave liveGpsPackets empty.
			// Clamp to histEndAutoRatioPct% of the range so there is always a live window.
			if cfg.UntilMS > 0 && cfg.UntilMS > cfg.FromMS && historyEndMS >= cfg.UntilMS {
				ratioPct := cfg.HistEndAutoRatioPct
				if ratioPct <= 0 || ratioPct >= 100 {
					ratioPct = 75
				}
				historyEndMS = cfg.FromMS + (cfg.UntilMS-cfg.FromMS)*int64(ratioPct)/100
				emit(SimEvent{
					Elapsed: elap(), Tag: "SPLIT", Cls: "warn",
					Msg: fmt.Sprintf("histEnd default (from+%dh) exceeds until — clamped to %d%% of range (%s)", defaultHours, ratioPct, time.UnixMilli(historyEndMS).UTC().Format("2006-01-02 15:04:05")),
					Ty: "warn",
				})
			}
		}

		for _, p := range allPackets {
			ptype := identifyPacketType(p.Packet)
			if p.T < historyEndMS {
				historicRows = append(historicRows, p.Packet)
			} else {
				livePkts = append(livePkts, livePacket{Packet: p.Packet, Type: ptype})
				switch ptype {
				case "gps":
					liveGpsPackets = append(liveGpsPackets, p.Packet)
				case "obd":
					liveObdPackets = append(liveObdPackets, p.Packet)
				}
			}
		}

		batchSize := cfg.BatchSize
		if batchSize <= 0 {
			batchSize = 30
		}

		var historicBatches [][]map[string]interface{}
		for i := 0; i < len(historicRows); i += batchSize {
			end := i + batchSize
			if end > len(historicRows) {
				end = len(historicRows)
			}
			historicBatches = append(historicBatches, historicRows[i:end])
		}
		if len(historicBatches) == 0 {
			historicBatches = append(historicBatches, []map[string]interface{}{})
		}

		totalLive := len(livePkts)
		emit(SimEvent{
			Elapsed: elap(),
			Tag:     "SPLIT",
			Cls:     "sp",
			Msg: fmt.Sprintf("Historic: %d  Live GPS: %d  OBD: %d  (total live: %d)",
				len(historicRows), len(liveGpsPackets), len(liveObdPackets), totalLive),
			Ty:   "ok",
			Step: "split:done",
			Data: map[string]interface{}{
				"historic":  len(historicRows),
				"liveGps":   len(liveGpsPackets),
				"liveObd":   len(liveObdPackets),
				"totalLive": totalLive,
				"batches":   len(historicBatches),
			},
		})
		runState.HistoricCount = len(historicRows)
		runState.LiveGpsCount = len(liveGpsPackets)
		runState.LiveObdCount = len(liveObdPackets)
		saveRunState(outDir, cfg.TgtIMEI, runState)

		if ctx.Err() != nil {
			return nil
		}

		// ── mqtt-pub mode: publish all packets directly via MQTT, no batch files ──
		if cfg.Mode == "mqtt-pub" {
			return runMQTTPubDirect(ctx, cfg, allPackets, emit, elap, s, startT)
		}

		// ── Step 3: Create batch files ─────────────────────────────────────────
		emit(SimEvent{
			Elapsed: elap(),
			Tag:     "BATCHES",
			Cls:     "bt",
			Msg: fmt.Sprintf("Creating %d historic SQLite files (encrypted=%v)…",
				len(historicBatches), cfg.EncryptEnabled),
			Ty:   "info",
			Step: "batch:creating",
			Data: map[string]interface{}{"total": len(historicBatches), "totalLive": totalLive},
		})

		for i, batch := range historicBatches {
			if ctx.Err() != nil {
				return nil
			}
			if err := s.checkPause(ctx); err != nil {
				return nil
			}
			dbPath := filepath.Join(outDir, fmt.Sprintf("historic_batch_%d_%s.db", i+1, cfg.TgtIMEI))
			if _, statErr := os.Stat(dbPath); os.IsNotExist(statErr) {
				if err := createBatchFile(i+1, batch, publicKeyPEM, dbPath, cfg, emit, startT); err != nil {
					emit(SimEvent{
						Elapsed: elap(), Tag: "BATCH", Cls: "bt",
						Msg: fmt.Sprintf("Batch %d failed: %v", i+1, err), Ty: "warn",
					})
				}
			} else {
				emit(SimEvent{
					Elapsed: elap(), Tag: "BATCH", Cls: "bt",
					Msg: fmt.Sprintf("Batch %d: reusing existing file", i+1), Ty: "info",
				})
			}
			batchFiles = append(batchFiles, dbPath)
		}

		emit(SimEvent{
			Elapsed: elap(),
			Tag:     "BATCH",
			Cls:     "bt",
			Msg:     fmt.Sprintf("All %d batch files ready", len(batchFiles)),
			Ty:      "ok",
			Step:    "batch:done",
			Data:    map[string]interface{}{"count": len(batchFiles)},
		})

		// Save livePkts to cache so future SkipFetch runs can skip the API fetch.
		if b, merr := json.Marshal(livePkts); merr == nil {
			os.WriteFile(livePktsPath, b, 0644) //nolint:errcheck
		}
	} // end !skipFetchDone

	// Update run-state totals after the full fetch path.
	if !skipFetchDone {
		runState.TotalBatches = len(batchFiles)
		runState.TotalOBDPackets = len(liveObdPackets)
		saveRunState(outDir, cfg.TgtIMEI, runState)
	} else {
		// Reconstruct GPS/OBD slices from cached livePkts (needed for GPS L1 goroutine).
		for _, lp := range livePkts {
			switch lp.Type {
			case "gps":
				liveGpsPackets = append(liveGpsPackets, lp.Packet)
			case "obd":
				liveObdPackets = append(liveObdPackets, lp.Packet)
			}
		}
	}

	if cfg.Mode == "fetch" {
		// Keep raw packets in memory so the frontend can export without decrypting SQLite files.
		s.StoreFetchedPackets(allPackets)
		// Fetch-only mode: done after creating batch files
		emit(SimEvent{
			Elapsed: elap(),
			Tag:     "DONE",
			Cls:     "ph",
			Msg:     fmt.Sprintf("Fetch complete: %d packets, %d batches", len(allPackets), len(batchFiles)),
			Ty:      "ok",
			Step:    "done",
			Data: map[string]interface{}{
				"durationSec": float64(elap()) / 1000.0,
				"metrics": map[string]interface{}{
					"totalPackets":     len(allPackets),
					"historicPackets":  len(historicRows),
					"liveGps":          len(liveGpsPackets),
					"liveObd":          len(liveObdPackets),
					"batchesUploaded":  0,
					"errors":           0,
				},
			},
		})
		return nil
	}

	// ── Step 4: MQTT connect ──────────────────────────────────────────────────
	if err := s.checkPause(ctx); err != nil {
		return nil
	}

	emit(SimEvent{
		Elapsed: elap(),
		Tag:     "MQTT",
		Cls:     "mq",
		Msg:     fmt.Sprintf("Connecting to %s://%s:%d (clientId:%s)…", cfg.MQTTProtocol, cfg.MQTTBroker, cfg.MQTTPort, cfg.TgtIMEI),
		Ty:      "info",
		Step:    "mqtt:connect",
	})

	var mqttPublish func(topic string, payload []byte) error
	var disconnectMQTT func()

	if cfg.DryRun {
		emit(SimEvent{
			Elapsed: elap(), Tag: "MQTT", Cls: "mq",
			Msg: "DRY RUN — skipping real MQTT", Ty: "info", Step: "mqtt:connected",
		})
		var dryCount int
		var dryMu sync.Mutex
		mqttPublish = func(topic string, payload []byte) error {
			dryMu.Lock()
			dryCount++
			n := dryCount
			dryMu.Unlock()
			if n%20 == 1 {
				emit(SimEvent{
					Elapsed: elap(), Tag: "DRY/MQTT", Cls: "mq",
					Msg: fmt.Sprintf("SKIP publish #%d → %s (%dB)", n, topic, len(payload)), Ty: "info",
				})
			}
			return nil
		}
		disconnectMQTT = func() {}
	} else {
		realClient, activeCfg, err := connectWithFallback(cfg, emit, elap)
		if err != nil {
			return err
		}
		cfg = activeCfg
		if ctx.Err() != nil {
			realClient.Disconnect(250)
			return nil
		}
		emit(SimEvent{
			Elapsed: elap(),
			Tag:     "MQTT",
			Cls:     "mq",
			Msg:     fmt.Sprintf("Connected  clientId:%s  broker:%s:%d", cfg.TgtIMEI, cfg.MQTTBroker, cfg.MQTTPort),
			Ty:      "ok",
			Step:    "mqtt:connected",
		})

		var (
			mqttClientMu sync.RWMutex
			mqttReconnMu sync.Mutex
			activeMQTT   mqtt.Client = realClient
		)

		doReconnect := func() error {
			mqttReconnMu.Lock()
			defer mqttReconnMu.Unlock()
			mqttClientMu.RLock()
			isConn := activeMQTT.IsConnected()
			currCfg := cfg
			mqttClientMu.RUnlock()
			if isConn {
				return nil
			}
			emit(SimEvent{Elapsed: elap(), Tag: "MQTT", Cls: "warn",
				Msg: "Broker disconnected — reconnecting in 3s…", Ty: "warn"})
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(3 * time.Second):
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			newClient, newCfg, rerr := connectWithFallback(currCfg, emit, elap)
			if rerr != nil {
				return fmt.Errorf("reconnect: %w", rerr)
			}
			mqttClientMu.Lock()
			old := activeMQTT
			activeMQTT = newClient
			cfg = newCfg
			mqttClientMu.Unlock()
			old.Disconnect(250)
			emit(SimEvent{Elapsed: elap(), Tag: "MQTT", Cls: "mq",
				Msg: fmt.Sprintf("Reconnected  clientId:%s  broker:%s:%d", newCfg.TgtIMEI, newCfg.MQTTBroker, newCfg.MQTTPort),
				Ty: "ok", Step: "mqtt:connected"})
			return nil
		}

		mqttPublish = func(topic string, payload []byte) error {
			mqttClientMu.RLock()
			c := activeMQTT
			isConn := c.IsConnected()
			mqttClientMu.RUnlock()
			if !isConn {
				if err := doReconnect(); err != nil {
					return err
				}
				mqttClientMu.RLock()
				c = activeMQTT
				mqttClientMu.RUnlock()
			}
			err := publishMQTT(c, topic, payload)
			if err != nil && errors.Is(err, mqtt.ErrNotConnected) {
				if err2 := doReconnect(); err2 != nil {
					return err2
				}
				mqttClientMu.RLock()
				c = activeMQTT
				mqttClientMu.RUnlock()
				return publishMQTT(c, topic, payload)
			}
			return err
		}

		disconnectMQTT = func() {
			mqttClientMu.RLock()
			c := activeMQTT
			mqttClientMu.RUnlock()
			c.Disconnect(250)
		}
	}
	defer disconnectMQTT()

	// ── Open OBD accumulation DB ──────────────────────────────────────────────
	obdDbPath := filepath.Join(outDir, fmt.Sprintf("live_obd_%s.db", cfg.TgtIMEI))
	obdDb, err := createOBDAccumDB(obdDbPath, cfg.EncryptEnabled)
	if err != nil {
		return fmt.Errorf("open obd db: %w", err)
	}

	// ── GPS L1 goroutine ─────────────────────────────────────────────────────
	// GPS L1 packets are sent continuously from Phase 1 start through Phase 2
	// OBD-file upload. Only when the OBD accumulated file is successfully
	// committed do we stop L1 and switch to normal-mode packets.
	var gpsL1Wg sync.WaitGroup
	gpsL1StopCh := make(chan struct{})
	var gpsL1Once sync.Once
	var gpsL1PubAtomic atomic.Int64
	closeGpsL1 := func() {
		gpsL1Once.Do(func() { close(gpsL1StopCh) })
		gpsL1Wg.Wait()
	}
	if !runState.Phase2OBDUploaded && len(liveGpsPackets) > 0 {
		gpsInterval := time.Duration(cfg.GPSl1IntervalMs) * time.Millisecond
		if gpsInterval <= 0 {
			gpsInterval = 10 * time.Second
		}
		gpsL1Topic := cfg.TgtIMEI + "/obd"
		gpsL1IdxFile := gpsL1StateFile(outDir, cfg.TgtIMEI)
		gpsL1Wg.Add(1)
		go func() {
			defer gpsL1Wg.Done()
			// Resume from saved position — cycles through liveGpsPackets infinitely.
			savedIdx := loadGpsL1Index(gpsL1IdxFile)
			gpsIdx := savedIdx % len(liveGpsPackets)
			published := 0
			naturalEnd := false // true only when closed via gpsL1StopCh (Phase 2 OBD upload done)
		gpsL1Loop:
			for {
				select {
				case <-ctx.Done():
					break gpsL1Loop
				case <-gpsL1StopCh:
					naturalEnd = true
					break gpsL1Loop
				default:
				}
				if err := s.checkPause(ctx); err != nil {
					break gpsL1Loop
				}
				if err := s.waitInterruptibleWithStop(ctx, gpsInterval, gpsL1StopCh); err != nil {
					// Distinguish: stop-channel fired (natural end) vs context cancelled (user stop).
					// Check the stop channel non-blockingly — if it's already closed it's a natural end
					// regardless of whether the context was also cancelled simultaneously.
					select {
					case <-gpsL1StopCh:
						naturalEnd = true
					default:
					}
					break gpsL1Loop
				}
				if ctx.Err() != nil {
					break gpsL1Loop
				}
				raw := liveGpsPackets[gpsIdx%len(liveGpsPackets)]
				gpsIdx++
				var pkt interface{}
				if cfg.EnrichL1 {
					pkt = convertToL1Packet(raw)
				} else {
					pkt = raw
				}
				data, _ := json.Marshal(pkt)
				if cfg.DryRun {
					published++
					gpsL1PubAtomic.Store(int64(published))
					saveGpsL1Index(gpsL1IdxFile, gpsIdx)
					emit(SimEvent{
						Elapsed: elap(), Tag: "P1/GPS", Cls: "mq",
						Msg:  fmt.Sprintf("GPS #%d → %s (dry run)", published, gpsL1Topic),
						Ty:   "info", Step: "p1:gps",
						Data: map[string]interface{}{
							"published": published,
							"total":     len(liveGpsPackets),
							"payload":   string(data),
						},
					})
				} else {
					if pubErr := mqttPublish(gpsL1Topic, data); pubErr != nil {
						emit(SimEvent{Elapsed: elap(), Tag: "P1/GPS", Cls: "er",
							Msg: fmt.Sprintf("publish error: %v", pubErr), Ty: "warn"})
					} else {
						published++
						gpsL1PubAtomic.Store(int64(published))
						saveGpsL1Index(gpsL1IdxFile, gpsIdx)
						emit(SimEvent{
							Elapsed: elap(), Tag: "P1/GPS", Cls: "mq",
							Msg:  fmt.Sprintf("GPS #%d → %s", published, gpsL1Topic),
							Ty:   "info", Step: "p1:gps",
							Data: map[string]interface{}{
								"published": published,
								"total":     len(liveGpsPackets),
								"payload":   string(data),
							},
						})
					}
				}
			}
			// Only delete the index file on natural end (closeGpsL1 called after Phase 2
			// OBD upload). On user-stop, keep it so GPS L1 resumes from the same position.
			if naturalEnd {
				os.Remove(gpsL1IdxFile) //nolint:errcheck
			}
			emit(SimEvent{
				Elapsed: elap(), Tag: "P1/GPS", Cls: "mq",
				Msg:  fmt.Sprintf("GPS L1 stream complete — %d published", published),
				Ty:   "info", Step: "p1:gps",
				Data: map[string]interface{}{"published": published, "total": len(liveGpsPackets)},
			})
		}()
	}

	// ── Phase 1 ───────────────────────────────────────────────────────────────
	if runState.Phase1Complete {
		emit(SimEvent{
			Elapsed: elap(), Tag: "PHASE 1", Cls: "ph",
			Msg:  "Phase 1 already complete — skipping to Phase 2",
			Ty:   "ph", Step: "phase1:done",
			Data: map[string]interface{}{"elapsed": elap()},
		})
		obdDb.Close()
	} else {
		emit(SimEvent{
			Elapsed: elap(),
			Tag:     "PHASE 1",
			Cls:     "ph",
			Msg:     "=== Phase 1: Historic upload + GPS L1 + OBD accumulation ===",
			Ty:      "ph",
			Step:    "phase1:start",
		})

		if err := s.checkPause(ctx); err != nil {
			obdDb.Close()
			return nil
		}

		if err := runPhase1(ctx, cfg, batchFiles, liveObdPackets, obdDb, publicKeyPEM, emit, startT, s, outDir, &runState); err != nil && ctx.Err() == nil {
			emit(SimEvent{Elapsed: elap(), Tag: "PHASE 1", Cls: "er", Msg: fmt.Sprintf("Phase 1 error: %v", err), Ty: "warn"})
		}
		obdDb.Close()

		if ctx.Err() == nil {
			runState.Phase1Complete = true
			saveRunState(outDir, cfg.TgtIMEI, runState)
		}
		emit(SimEvent{
			Elapsed: elap(),
			Tag:     "PHASE 1",
			Cls:     "ph",
			Msg:     fmt.Sprintf("=== Phase 1 complete in %.1fs ===", float64(elap())/1000.0),
			Ty:      "ok",
			Step:    "phase1:done",
			Data:    map[string]interface{}{"elapsed": elap()},
		})
	}

	if ctx.Err() != nil {
		return nil
	}

	// ── Phase 2 ───────────────────────────────────────────────────────────────
	emit(SimEvent{
		Elapsed: elap(),
		Tag:     "PHASE 2",
		Cls:     "ph",
		Msg:     "=== Phase 2: OBD upload + Normal-mode streaming ===",
		Ty:      "ph",
		Step:    "phase2:start",
	})

	if err := s.checkPause(ctx); err != nil {
		return nil
	}

	// Seed live normal GPS/OBD intervals; fall back to legacy single-interval field.
	gpsNormalMs := cfg.NormalGPSIntervalMs
	if gpsNormalMs <= 0 {
		gpsNormalMs = cfg.NormalIntervalMs
	}
	obdNormalMs := cfg.NormalOBDIntervalMs
	if obdNormalMs <= 0 {
		obdNormalMs = cfg.NormalIntervalMs
	}
	s.SetNormalIntervals(gpsNormalMs, obdNormalMs)

	if err := runPhase2(ctx, cfg, obdDbPath, livePkts, mqttPublish, emit, startT, s, &runState, outDir, cfg.TgtIMEI, closeGpsL1); err != nil && ctx.Err() == nil {
		emit(SimEvent{Elapsed: elap(), Tag: "PHASE 2", Cls: "er", Msg: fmt.Sprintf("Phase 2 error: %v", err), Ty: "warn"})
	}
	// GPS L1 is fully stopped after runPhase2 (closeGpsL1 was called inside).
	runState.GpsL1Published = int(gpsL1PubAtomic.Load())
	saveRunState(outDir, cfg.TgtIMEI, runState)

	emit(SimEvent{
		Elapsed: elap(),
		Tag:     "PHASE 2",
		Cls:     "ph",
		Msg:     "=== Phase 2 complete ===",
		Ty:      "ok",
		Step:    "phase2:done",
	})

	// ── Done ──────────────────────────────────────────────────────────────────
	durationSec := float64(elap()) / 1000.0
	emit(SimEvent{
		Elapsed: elap(),
		Tag:     "DONE",
		Cls:     "ph",
		Msg:     fmt.Sprintf("════ Simulation complete in %.1fs ════", durationSec),
		Ty:      "ok",
		Step:    "done",
		Data: map[string]interface{}{
			"durationSec": durationSec,
			"metrics": map[string]interface{}{
				"totalPackets":    len(allPackets),
				"historicPackets": len(historicRows),
				"liveGps":         len(liveGpsPackets),
				"liveObd":         len(liveObdPackets),
				"batchesUploaded": len(batchFiles),
				"errors":          0,
			},
		},
	})
	return nil
}

// runPhase1 runs two concurrent streams:
// A: sequential historic batch uploads
// C: OBD accumulation into SQLite (interval-based, stops when A finishes)
// GPS L1 streaming is managed by runSimulation so it spans Phase 1 and
// Phase 2 OBD upload, stopping only after OBD history is committed.
func runPhase1(ctx context.Context, cfg Config, batchFiles []string, liveObdPackets []map[string]interface{}, obdDb *sql.DB, publicKeyPEM string, emit func(SimEvent), startT time.Time, s *Simulator, outDir string, runState *simRunState) error {
	elap := func() int64 { return time.Since(startT).Milliseconds() }
	phaseStart := time.Now()

	batchUploadDelay := time.Duration(cfg.BatchUploadDelayMs) * time.Millisecond
	if batchUploadDelay <= 0 {
		batchUploadDelay = 120 * time.Second
	}
	obdInterval := time.Duration(cfg.OBDAccumIntervalMs) * time.Millisecond
	if obdInterval <= 0 {
		obdInterval = 120 * time.Second
	}

	// Phase 1 done signal
	phase1Done := make(chan struct{})
	var phase1Once sync.Once
	signalDone := func() { phase1Once.Do(func() { close(phase1Done) }) }

	var wg sync.WaitGroup

	// ── Stream A: sequential historic uploads ───────────────────────────────
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer signalDone()

		for i, dbPath := range batchFiles {
			if ctx.Err() != nil {
				return
			}
			if err := s.checkPause(ctx); err != nil {
				return
			}

			// Skip batches already uploaded in a previous run
			if i < runState.BatchesUploaded {
				emit(SimEvent{
					Elapsed: elap(), Tag: "UPLOAD", Cls: "up",
					Msg:  fmt.Sprintf("SKIP batch_%d/%d (already uploaded)", i+1, len(batchFiles)),
					Ty:   "info", Step: "p1:upload",
					Data: map[string]interface{}{"current": i + 1, "total": len(batchFiles), "n": i + 1},
				})
				continue
			}

			if cfg.DryRun {
				emit(SimEvent{
					Elapsed: elap(), Tag: "DRY/UPLOAD", Cls: "up",
					Msg:  fmt.Sprintf("SKIP upload: batch_%d (dry run)", i+1),
					Ty:   "info", Step: "p1:upload",
					Data: map[string]interface{}{"current": i + 1, "total": len(batchFiles), "n": i + 1},
				})
				time.Sleep(200 * time.Millisecond)
			} else {
				t0 := time.Now()
				err := uploadFile(dbPath, cfg)
				elapsed := time.Since(t0)
				if err != nil {
					emit(SimEvent{
						Elapsed: elap(), Tag: "UPLOAD", Cls: "er",
						Msg: fmt.Sprintf("batch_%d FAILED: %v", i+1, err), Ty: "warn",
					})
				} else {
					emit(SimEvent{
						Elapsed: elap(), Tag: "UPLOAD", Cls: "up",
						Msg:  fmt.Sprintf("OK 200 — batch_%d/%d in %dms", i+1, len(batchFiles), elapsed.Milliseconds()),
						Ty:   "ok", Step: "p1:upload",
						Data: map[string]interface{}{"current": i + 1, "total": len(batchFiles), "n": i + 1},
					})
				}
			}

			// Persist upload progress so resume works after a stop
			runState.BatchesUploaded = i + 1
			saveRunState(outDir, cfg.TgtIMEI, *runState)

			if i < len(batchFiles)-1 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(batchUploadDelay):
				}
			}
		}

		emit(SimEvent{
			Elapsed: elap(), Tag: "P1/UPLOAD", Cls: "up",
			Msg: fmt.Sprintf("All %d batches uploaded — signalling streams to stop", len(batchFiles)), Ty: "ok",
		})
	}()

	// ── Stream C: OBD accumulation ───────────────────────────────────────────
	wg.Add(1)
	go func() {
		defer wg.Done()
		if len(liveObdPackets) == 0 {
			<-phase1Done
			return
		}

		// Resume OBD accumulation from where the previous run stopped.
		var obdIdx int
		obdDb.QueryRow("SELECT COUNT(*) FROM oData").Scan(&obdIdx) //nolint:errcheck
		if obdIdx > 0 && obdIdx < len(liveObdPackets) {
			emit(SimEvent{
				Elapsed: elap(), Tag: "P1/OBD", Cls: "bt",
				Msg: fmt.Sprintf("Resuming OBD accumulation from row %d/%d", obdIdx+1, len(liveObdPackets)),
				Ty:  "info", Step: "p1:obd",
				Data: map[string]interface{}{"rows": obdIdx, "total": len(liveObdPackets)},
			})
		}
		insertErrors := 0
		ticker := time.NewTicker(obdInterval)
		defer ticker.Stop()

		for obdIdx < len(liveObdPackets) {
			select {
			case <-ctx.Done():
				return
			case <-phase1Done:
				goto streamCDone
			case <-ticker.C:
			}

			if err := s.checkPause(ctx); err != nil {
				return
			}

			if cfg.DryRun {
				obdIdx++
				emit(SimEvent{
					Elapsed: elap(), Tag: "DRY/OBD", Cls: "bt",
					Msg:  fmt.Sprintf("SKIP OBD row %d/%d", obdIdx, len(liveObdPackets)),
					Ty:   "info",
					Step: "p1:obd",
					Data: map[string]interface{}{"rows": obdIdx, "total": len(liveObdPackets)},
				})
			} else {
				pkt := liveObdPackets[obdIdx]
				if err := insertOBDRow(obdDb, pkt, publicKeyPEM, cfg); err != nil {
					insertErrors++
					emit(SimEvent{Elapsed: elap(), Tag: "P1/OBD", Cls: "er", Msg: fmt.Sprintf("insert error: %v", err), Ty: "warn"})
				}
				obdIdx++
				emit(SimEvent{
					Elapsed: elap(), Tag: "P1/OBD", Cls: "bt",
					Msg:  fmt.Sprintf("Accumulated OBD row %d/%d", obdIdx, len(liveObdPackets)),
					Ty:   "info",
					Step: "p1:obd",
					Data: map[string]interface{}{"rows": obdIdx, "total": len(liveObdPackets)},
				})
			}
		}
		<-phase1Done

	streamCDone:
		emit(SimEvent{
			Elapsed: elap(), Tag: "P1/OBD", Cls: "bt",
			Msg: fmt.Sprintf("OBD accumulation stopped — total: %d (errors: %d)", obdIdx, insertErrors), Ty: "info",
		})
	}()

	wg.Wait()

	_ = time.Since(phaseStart)
	return nil
}

// runNaturalOrderStream streams livePkts in original time-sorted order (GPS and OBD
// interleaved as fetched) using a single configured delay between every packet.
// Resume uses LiveStreamOffset — the sequential index into livePkts.
func runNaturalOrderStream(ctx context.Context, cfg Config, livePkts []livePacket, topic string, publish func(string, []byte) error, emit func(SimEvent), elap func() int64, s *Simulator, runState *simRunState, outDir, tgtIMEI string) error {
	total := len(livePkts)
	startIdx := runState.LiveStreamOffset

	interval := time.Duration(cfg.NaturalOrderIntervalMs) * time.Millisecond
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}

	fmtDelay := func(d time.Duration) string {
		if d < time.Second {
			return fmt.Sprintf("%dms", d.Milliseconds())
		}
		return fmt.Sprintf("%.0fs", d.Seconds())
	}

	gpsCount, obdCount := 0, 0
	// Pre-count totals for remaining display
	totalGps, totalObd := 0, 0
	for _, lp := range livePkts {
		switch lp.Type {
		case "gps":
			totalGps++
		case "obd":
			totalObd++
		}
	}

	if startIdx > 0 && startIdx < total {
		for _, lp := range livePkts[:startIdx] {
			switch lp.Type {
			case "gps":
				gpsCount++
			case "obd":
				obdCount++
			}
		}
		emit(SimEvent{
			Elapsed: elap(), Tag: "P2/NAT", Cls: "fe",
			Msg:  fmt.Sprintf("Resuming natural order from #%d/%d (gps:%d obd:%d)", startIdx+1, total, gpsCount, obdCount),
			Ty:   "info", Step: "p2:stream",
			Data: map[string]interface{}{
				"current": startIdx, "total": total,
				"gps": gpsCount, "obd": obdCount,
				"totalGps": totalGps, "totalObd": totalObd,
			},
		})
	}

	for i := startIdx; i < total; i++ {
		if ctx.Err() != nil {
			return nil
		}
		if err := s.checkPause(ctx); err != nil {
			return nil
		}
		lp := livePkts[i]
		pkt := lp.Packet
		if lp.Type == "gps" {
			pkt = toNormalPacket(pkt) // strip l:"1" — Phase 2 always sends normal GPS
		}
		data, _ := json.Marshal(pkt)

		pktTag := "P2/GPS"
		pktCls := "mq"
		if lp.Type == "obd" {
			pktTag = "P2/OBD"
			pktCls = "bt"
			obdCount++
		} else {
			gpsCount++
		}

		nextInfo := ""
		if i < total-1 {
			nextInfo = fmt.Sprintf(" — next in %s", fmtDelay(interval))
		}

		if cfg.DryRun {
			emit(SimEvent{
				Elapsed: elap(), Tag: pktTag, Cls: pktCls,
				Msg:  fmt.Sprintf("#%d/%d → %s (dry run)%s", i+1, total, topic, nextInfo),
				Ty:   "info", Step: "p2:stream",
				Data: map[string]interface{}{
					"payload": string(data),
					"current": i + 1, "total": total,
					"gps": gpsCount, "obd": obdCount,
					"totalGps": totalGps, "totalObd": totalObd,
				},
			})
		} else {
			if err := publish(topic, data); err != nil {
				emit(SimEvent{Elapsed: elap(), Tag: pktTag, Cls: "er",
					Msg: fmt.Sprintf("publish error pkt %d: %v", i+1, err), Ty: "warn"})
			} else {
				emit(SimEvent{
					Elapsed: elap(), Tag: pktTag, Cls: pktCls,
					Msg:  fmt.Sprintf("#%d/%d → %s%s", i+1, total, topic, nextInfo),
					Ty:   "info", Step: "p2:stream",
					Data: map[string]interface{}{
						"payload": string(data),
						"current": i + 1, "total": total,
						"gps": gpsCount, "obd": obdCount,
						"totalGps": totalGps, "totalObd": totalObd,
					},
				})
			}
		}

		runState.LiveStreamOffset = i + 1
		saveRunState(outDir, tgtIMEI, *runState)

		if i < total-1 {
			if s.waitInterruptible(ctx, interval) != nil {
				return nil
			}
		}
	}

	emit(SimEvent{
		Elapsed: elap(), Tag: "P2/NAT", Cls: "fe",
		Msg:  fmt.Sprintf("Natural order complete — GPS: %d, OBD: %d", gpsCount, obdCount),
		Ty:   "ok", Step: "p2:stream",
		Data: map[string]interface{}{
			"current": total, "total": total,
			"gps": gpsCount, "obd": obdCount,
			"totalGps": totalGps, "totalObd": totalObd,
		},
	})
	return nil
}

// runPhase2 runs the two sequential steps:
// 1. Upload the accumulated OBD database
// 2. Stream all live packets via MQTT at normal interval
func runPhase2(ctx context.Context, cfg Config, obdDbPath string, livePkts []livePacket, publish func(string, []byte) error, emit func(SimEvent), startT time.Time, s *Simulator, runState *simRunState, outDir, tgtIMEI string, closeGpsL1 func()) error {
	elap := func() int64 { return time.Since(startT).Milliseconds() }
	topic := cfg.TgtIMEI + "/obd"

	// 1. Upload OBD DB — skip if already uploaded in a previous run.
	// Normal-mode streaming is BLOCKED until this succeeds; GPS L1 continues
	// in the background throughout all retry attempts.
	if runState.Phase2OBDUploaded {
		emit(SimEvent{
			Elapsed: elap(), Tag: "P2/UPLOAD", Cls: "up",
			Msg: "OBD DB already uploaded — skipping", Ty: "info", Step: "p2:upload:done",
		})
	} else if cfg.DryRun {
		emit(SimEvent{
			Elapsed: elap(), Tag: "DRY/UPLOAD", Cls: "up",
			Msg: fmt.Sprintf("SKIP upload: live_obd_%s.db (dry run)", cfg.TgtIMEI), Ty: "info",
		})
		time.Sleep(200 * time.Millisecond)
		emit(SimEvent{
			Elapsed: elap(), Tag: "P2/UPLOAD", Cls: "up",
			Msg: "OBD DB upload complete (dry run)", Ty: "ok", Step: "p2:upload:done",
		})
	} else {
		const retryDelay = 30 * time.Second
		for attempt := 1; ; attempt++ {
			if ctx.Err() != nil {
				closeGpsL1()
				return nil
			}
			t0 := time.Now()
			if err := uploadFile(obdDbPath, cfg); err != nil {
				emit(SimEvent{
					Elapsed: elap(), Tag: "P2/UPLOAD", Cls: "er",
					Msg: fmt.Sprintf("OBD upload attempt %d FAILED: %v — retrying in %.0fs (GPS L1 continues)", attempt, err, retryDelay.Seconds()),
					Ty: "warn",
				})
				if s.waitInterruptible(ctx, retryDelay) != nil {
					closeGpsL1()
					return nil
				}
				continue
			}
			emit(SimEvent{
				Elapsed: elap(), Tag: "P2/UPLOAD", Cls: "up",
				Msg: fmt.Sprintf("OK 200 — live_obd_%s.db in %dms (attempt %d)", cfg.TgtIMEI, time.Since(t0).Milliseconds(), attempt),
				Ty:  "ok",
			})
			runState.Phase2OBDUploaded = true
			saveRunState(outDir, tgtIMEI, *runState)
			emit(SimEvent{
				Elapsed: elap(), Tag: "P2/UPLOAD", Cls: "up",
				Msg: "OBD DB upload complete", Ty: "ok", Step: "p2:upload:done",
			})
			break
		}
	}

	// GPS L1 has served its purpose — signal it to stop before normal mode begins.
	closeGpsL1()

	if ctx.Err() != nil {
		return nil
	}

	// 2. Natural Order mode — single goroutine, time-sorted original order, one delay.
	if cfg.NormalStreamMode == "natural" {
		return runNaturalOrderStream(ctx, cfg, livePkts, topic, publish, emit, elap, s, runState, outDir, tgtIMEI)
	}

	// 3. Independent Streams mode (default) — GPS and OBD on separate goroutines so each
	// fires at its own configured interval without blocking the other.
	var gpsQueue []livePacket
	var obdQueue []livePacket
	for _, lp := range livePkts {
		switch lp.Type {
		case "gps":
			gpsQueue = append(gpsQueue, lp)
		case "obd":
			obdQueue = append(obdQueue, lp)
		}
	}
	totalGps := len(gpsQueue)
	totalObd := len(obdQueue)
	total := totalGps + totalObd

	// Format sub-second delays as ms, ≥1s as seconds
	fmtDelay := func(d time.Duration) string {
		if d < time.Second {
			return fmt.Sprintf("%dms", d.Milliseconds())
		}
		return fmt.Sprintf("%.0fs", d.Seconds())
	}

	// Atomic sent counters shared between both goroutines for progress events.
	var gpsSentA, obdSentA atomic.Int64

	// Mutex for runState saves so both goroutines don't race on the file.
	var stateMu sync.Mutex
	saveState := func() {
		stateMu.Lock()
		saveRunState(outDir, tgtIMEI, *runState)
		stateMu.Unlock()
	}

	if runState.LiveGpsOffset > 0 || runState.LiveObdOffset > 0 {
		emit(SimEvent{
			Elapsed: elap(), Tag: "P2/STREAM", Cls: "fe",
			Msg: fmt.Sprintf("Resuming — GPS from #%d/%d  OBD from #%d/%d",
				runState.LiveGpsOffset+1, totalGps, runState.LiveObdOffset+1, totalObd),
			Ty: "info", Step: "p2:stream",
			Data: map[string]interface{}{
				"current": runState.LiveGpsOffset + runState.LiveObdOffset,
				"total": total, "gps": runState.LiveGpsOffset, "obd": runState.LiveObdOffset,
				"totalGps": totalGps, "totalObd": totalObd,
			},
		})
	}

	var wg sync.WaitGroup
	wg.Add(2)

	// GPS goroutine
	go func() {
		defer wg.Done()
		start := runState.LiveGpsOffset
		for i := start; i < len(gpsQueue); i++ {
			if ctx.Err() != nil {
				return
			}
			if err := s.checkPause(ctx); err != nil {
				return
			}
			lp := gpsQueue[i]
			pkt := toNormalPacket(lp.Packet)
			data, _ := json.Marshal(pkt)
			gpsSentA.Add(1)
			gs := int(gpsSentA.Load())
			os2 := int(obdSentA.Load())
			interval := s.getLiveNormalGpsInterval()
			nextInfo := ""
			if i < len(gpsQueue)-1 {
				nextInfo = fmt.Sprintf(" — next in %s", fmtDelay(interval))
			}
			if cfg.DryRun {
				emit(SimEvent{
					Elapsed: elap(), Tag: "P2/GPS", Cls: "mq",
					Msg:  fmt.Sprintf("#%d/%d → %s (dry run)%s", gs, totalGps, topic, nextInfo),
					Ty:   "info", Step: "p2:stream",
					Data: map[string]interface{}{
						"payload": string(data),
						"current": gs + os2, "total": total,
						"gps": gs, "obd": os2,
						"totalGps": totalGps, "totalObd": totalObd,
					},
				})
			} else {
				if err := publish(topic, data); err != nil {
					emit(SimEvent{Elapsed: elap(), Tag: "P2/GPS", Cls: "er",
						Msg: fmt.Sprintf("GPS publish error #%d: %v", gs, err), Ty: "warn"})
				} else {
					emit(SimEvent{
						Elapsed: elap(), Tag: "P2/GPS", Cls: "mq",
						Msg:  fmt.Sprintf("#%d/%d → %s%s", gs, totalGps, topic, nextInfo),
						Ty:   "info", Step: "p2:stream",
						Data: map[string]interface{}{
							"payload": string(data),
							"current": gs + os2, "total": total,
							"gps": gs, "obd": os2,
							"totalGps": totalGps, "totalObd": totalObd,
						},
					})
				}
			}
			stateMu.Lock()
			runState.LiveGpsOffset = i + 1
			stateMu.Unlock()
			saveState()
			if i < len(gpsQueue)-1 {
				if s.waitInterruptible(ctx, interval) != nil {
					return
				}
			}
		}
	}()

	// OBD goroutine
	go func() {
		defer wg.Done()
		start := runState.LiveObdOffset
		for i := start; i < len(obdQueue); i++ {
			if ctx.Err() != nil {
				return
			}
			if err := s.checkPause(ctx); err != nil {
				return
			}
			lp := obdQueue[i]
			data, _ := json.Marshal(lp.Packet)
			obdSentA.Add(1)
			gs := int(gpsSentA.Load())
			os2 := int(obdSentA.Load())
			interval := s.getLiveNormalObdInterval()
			nextInfo := ""
			if i < len(obdQueue)-1 {
				nextInfo = fmt.Sprintf(" — next in %s", fmtDelay(interval))
			}
			if cfg.DryRun {
				emit(SimEvent{
					Elapsed: elap(), Tag: "P2/OBD", Cls: "bt",
					Msg:  fmt.Sprintf("#%d/%d → %s (dry run)%s", os2, totalObd, topic, nextInfo),
					Ty:   "info", Step: "p2:stream",
					Data: map[string]interface{}{
						"payload": string(data),
						"current": gs + os2, "total": total,
						"gps": gs, "obd": os2,
						"totalGps": totalGps, "totalObd": totalObd,
					},
				})
			} else {
				if err := publish(topic, data); err != nil {
					emit(SimEvent{Elapsed: elap(), Tag: "P2/OBD", Cls: "er",
						Msg: fmt.Sprintf("OBD publish error #%d: %v", os2, err), Ty: "warn"})
				} else {
					emit(SimEvent{
						Elapsed: elap(), Tag: "P2/OBD", Cls: "bt",
						Msg:  fmt.Sprintf("#%d/%d → %s%s", os2, totalObd, topic, nextInfo),
						Ty:   "info", Step: "p2:stream",
						Data: map[string]interface{}{
							"payload": string(data),
							"current": gs + os2, "total": total,
							"gps": gs, "obd": os2,
							"totalGps": totalGps, "totalObd": totalObd,
						},
					})
				}
			}
			stateMu.Lock()
			runState.LiveObdOffset = i + 1
			stateMu.Unlock()
			saveState()
			if i < len(obdQueue)-1 {
				if s.waitInterruptible(ctx, interval) != nil {
					return
				}
			}
		}
	}()

	wg.Wait()

	gs := int(gpsSentA.Load())
	os2 := int(obdSentA.Load())
	emit(SimEvent{
		Elapsed: elap(), Tag: "P2/STREAM", Cls: "fe",
		Msg:  fmt.Sprintf("Normal mode complete — GPS: %d, OBD: %d", gs, os2),
		Ty:   "ok", Step: "p2:stream",
		Data: map[string]interface{}{
			"current": gs + os2, "total": total,
			"gps": gs, "obd": os2,
			"totalGps": totalGps, "totalObd": totalObd,
		},
	})
	return nil
}

// noopMQTT is a no-op MQTT client for dry runs.
type noopMQTT struct{}

func (n *noopMQTT) Disconnect(_ uint) {}

// runMQTTPubDirect implements the "mqtt-pub" mode: fetch → split → publish all
// packets directly to MQTT without creating batch files or HTTP uploads.
func runMQTTPubDirect(ctx context.Context, cfg Config, allPackets []Packet, emit func(SimEvent), elap func() int64, s *Simulator, startT time.Time) error {
	topic := cfg.TgtIMEI + "/obd"

	// Split into independent GPS and OBD streams — filter out handshake/unknown.
	var gpsPackets, obdPackets []Packet
	for _, p := range allPackets {
		switch identifyPacketType(p.Packet) {
		case "gps":
			gpsPackets = append(gpsPackets, p)
		case "obd":
			obdPackets = append(obdPackets, p)
		}
	}
	gpsTotal, obdTotal := len(gpsPackets), len(obdPackets)
	total := gpsTotal + obdTotal

	// Seed live intervals so real-time slider updates work immediately.
	s.SetLiveIntervals(cfg.GPSl1IntervalMs, cfg.OBDAccumIntervalMs)
	dryRun := cfg.DryRun

	emit(SimEvent{
		Elapsed: elap(), Tag: "MQTT", Cls: "mq",
		Msg:  fmt.Sprintf("Connecting to %s://%s:%d (clientId:%s)…", cfg.MQTTProtocol, cfg.MQTTBroker, cfg.MQTTPort, cfg.TgtIMEI),
		Ty:   "info", Step: "mqtt:connect",
	})

	// ── MQTT client shared between both goroutines ──────────────────────────
	var (
		clientMu  sync.RWMutex  // guards activeClient + cfg reads/writes
		reconnMu  sync.Mutex    // serialises reconnect so only one goroutine reconnects
		activeClient mqtt.Client
	)

	var publish func(string, []byte) error

	if dryRun {
		emit(SimEvent{Elapsed: elap(), Tag: "MQTT", Cls: "mq",
			Msg: "DRY RUN — skipping real MQTT", Ty: "info", Step: "mqtt:connected"})
		var dryN atomic.Int64
		publish = func(t string, p []byte) error {
			n := dryN.Add(1)
			if n%20 == 1 {
				emit(SimEvent{Elapsed: elap(), Tag: "DRY/MQTT", Cls: "mq",
					Msg: fmt.Sprintf("SKIP #%d → %s (%dB)", n, t, len(p)), Ty: "info"})
			}
			return nil
		}
		defer (&noopMQTT{}).Disconnect(250)
	} else {
		realClient, activeCfg, err := connectWithFallback(cfg, emit, elap)
		if err != nil {
			return err
		}
		cfg = activeCfg
		if ctx.Err() != nil {
			realClient.Disconnect(250)
			return nil
		}
		emit(SimEvent{Elapsed: elap(), Tag: "MQTT", Cls: "mq",
			Msg: fmt.Sprintf("Connected  clientId:%s  broker:%s:%d", cfg.TgtIMEI, cfg.MQTTBroker, cfg.MQTTPort),
			Ty: "ok", Step: "mqtt:connected"})
		activeClient = realClient
		defer func() {
			clientMu.RLock()
			c := activeClient
			clientMu.RUnlock()
			c.Disconnect(250)
		}()

		doReconnect := func() error {
			reconnMu.Lock()
			defer reconnMu.Unlock()
			// Double-check: another goroutine may have reconnected while we waited.
			clientMu.RLock()
			isConn := activeClient.IsConnected()
			currCfg := cfg
			clientMu.RUnlock()
			if isConn {
				return nil
			}
			emit(SimEvent{Elapsed: elap(), Tag: "MQTT", Cls: "warn",
				Msg: "Broker disconnected — reconnecting in 3s…", Ty: "warn"})
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(3 * time.Second):
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			newClient, newCfg, rerr := connectWithFallback(currCfg, emit, elap)
			if rerr != nil {
				return fmt.Errorf("reconnect: %w", rerr)
			}
			clientMu.Lock()
			old := activeClient
			activeClient = newClient
			cfg = newCfg
			clientMu.Unlock()
			old.Disconnect(250)
			emit(SimEvent{Elapsed: elap(), Tag: "MQTT", Cls: "mq",
				Msg: fmt.Sprintf("Reconnected  clientId:%s  broker:%s:%d", newCfg.TgtIMEI, newCfg.MQTTBroker, newCfg.MQTTPort),
				Ty: "ok", Step: "mqtt:connected"})
			return nil
		}

		publish = func(t string, p []byte) error {
			clientMu.RLock()
			c := activeClient
			isConn := c.IsConnected()
			clientMu.RUnlock()
			if !isConn {
				if err := doReconnect(); err != nil {
					return err
				}
				clientMu.RLock()
				c = activeClient
				clientMu.RUnlock()
			}
			err := publishMQTT(c, t, p)
			if err != nil && errors.Is(err, mqtt.ErrNotConnected) {
				if err2 := doReconnect(); err2 != nil {
					return err2
				}
				clientMu.RLock()
				c = activeClient
				clientMu.RUnlock()
				return publishMQTT(c, t, p)
			}
			return err
		}
	}

	emit(SimEvent{
		Elapsed: elap(), Tag: "MQTT PUB", Cls: "mq",
		Msg: fmt.Sprintf("Starting: %d packets (GPS: %d, OBD: %d) → %s", total, gpsTotal, obdTotal, topic),
		Ty:  "info", Step: "mqttpub:start",
		Data: map[string]interface{}{"total": total, "gpsTotal": gpsTotal, "obdTotal": obdTotal},
	})

	// ── Concurrent GPS + OBD publish — each stream runs independently ───────
	var pubGps, pubObd, errGps, errObd atomic.Int64

	runStream := func(pkts []Packet, tag, cls string, getDelay func() time.Duration, pubCnt, errCnt *atomic.Int64) {
		n := len(pkts)
		for i, pkt := range pkts {
			if ctx.Err() != nil {
				return
			}
			if i%50 == 0 {
				if err := s.checkPause(ctx); err != nil {
					return
				}
			}
			data, _ := json.Marshal(pkt.Packet)
			if err := publish(topic, data); err != nil {
				errCnt.Add(1)
				emit(SimEvent{Elapsed: elap(), Tag: tag, Cls: "er",
					Msg: fmt.Sprintf("publish error pkt %d: %v", i+1, err), Ty: "warn"})
			} else {
				pubCnt.Add(1)
			}

			var delay time.Duration
			if !dryRun && i < n-1 {
				delay = getDelay()
			}
			allGps := int(pubGps.Load())
			allObd := int(pubObd.Load())
			msg := fmt.Sprintf("#%d/%d", i+1, n)
			if delay > 0 {
				msg += fmt.Sprintf(" — next in %.1fs", delay.Seconds())
			}
			emit(SimEvent{
				Elapsed: elap(), Tag: tag, Cls: cls,
				Msg: msg, Ty: "info", Step: "mqttpub:packet",
				Data: map[string]interface{}{
					"current": allGps + allObd, "total": total,
					"gps": allGps, "obd": allObd,
					"gpsTotal": gpsTotal, "obdTotal": obdTotal,
					"payload": string(data),
				},
			})
			if delay > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(delay):
				}
			}
		}
	}

	var wg sync.WaitGroup
	if gpsTotal > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runStream(gpsPackets, "GPS", "up", func() time.Duration {
				gpsMs, _ := s.getLiveIntervals()
				d := time.Duration(gpsMs) * time.Millisecond
				if d <= 0 {
					d = 10 * time.Second
				}
				return d
			}, &pubGps, &errGps)
		}()
	}
	if obdTotal > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runStream(obdPackets, "OBD", "bt", func() time.Duration {
				_, obdMs := s.getLiveIntervals()
				d := time.Duration(obdMs) * time.Millisecond
				if d <= 0 {
					d = 5 * time.Second
				}
				return d
			}, &pubObd, &errObd)
		}()
	}
	wg.Wait()

	gps := int(pubGps.Load())
	obd := int(pubObd.Load())
	errs := int(errGps.Load() + errObd.Load())
	emit(SimEvent{
		Elapsed: elap(), Tag: "DONE", Cls: "ph",
		Msg: fmt.Sprintf("MQTT Direct complete — GPS: %d/%d  OBD: %d/%d  errors: %d",
			gps, gpsTotal, obd, obdTotal, errs),
		Ty:   "ok", Step: "done",
		Data: map[string]interface{}{
			"durationSec": float64(elap()) / 1000.0,
			"metrics": map[string]interface{}{
				"totalPackets": total, "historicPackets": 0,
				"liveGps": gps, "liveObd": obd,
				"batchesUploaded": 0, "errors": errs,
			},
		},
	})
	return nil
}

