package simulator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// EventEmitter is a function that emits a SimEvent to the frontend.
type EventEmitter func(SimEvent)

// Simulator manages the simulation lifecycle.
type Simulator struct {
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	paused   bool
	startT   time.Time

	// Live-updatable intervals for MQTT Direct mode (updated while sim is running).
	liveMu    sync.RWMutex
	liveGpsMs int
	liveObdMs int
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

	// ── Step 1: Fetch ─────────────────────────────────────────────────────────
	if err := s.checkPause(ctx); err != nil {
		return nil
	}
	allPackets, err := fetchTelemetry(ctx, cfg, emit, startT)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	if ctx.Err() != nil {
		return nil
	}

	// ── Step 2: Split ─────────────────────────────────────────────────────────
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
		historyEndMS = cfg.FromMS + 12*3600*1000
	}

	var historicRows []map[string]interface{}
	var liveGpsPackets []map[string]interface{}
	var liveObdPackets []map[string]interface{}
	var livePkts []livePacket

	for _, p := range allPackets {
		ptype := identifyPacketType(p.Packet)
		if p.T < historyEndMS {
			historicRows = append(historicRows, p.Packet)
		} else {
			livePkts = append(livePkts, livePacket{Packet: p.Packet, Type: ptype})
			if ptype == "gps" {
				liveGpsPackets = append(liveGpsPackets, p.Packet)
			} else if ptype == "obd" {
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

	if ctx.Err() != nil {
		return nil
	}

	// ── mqtt-pub mode: publish all packets directly via MQTT, no batch files ──
	if cfg.Mode == "mqtt-pub" {
		return runMQTTPubDirect(ctx, cfg, allPackets, emit, elap, s, startT)
	}

	// ── Step 3: Create batch files ───────────────────────────────────────────
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

	var batchFiles []string
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

	if cfg.Mode == "fetch" {
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

	var mqttClient interface{ Disconnect(uint) }
	var mqttPublish func(topic string, payload []byte) error

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
		mqttClient = &noopMQTT{}
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
		mqttPublish = func(topic string, payload []byte) error {
			return publishMQTT(realClient, topic, payload)
		}
		mqttClient = realClient
	}
	defer mqttClient.Disconnect(250)

	// ── Open OBD accumulation DB ──────────────────────────────────────────────
	obdDbPath := filepath.Join(outDir, fmt.Sprintf("live_obd_%s.db", cfg.TgtIMEI))
	obdDb, err := createOBDAccumDB(obdDbPath, cfg.EncryptEnabled)
	if err != nil {
		return fmt.Errorf("open obd db: %w", err)
	}

	// ── Phase 1 ───────────────────────────────────────────────────────────────
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

	if err := runPhase1(ctx, cfg, batchFiles, liveGpsPackets, liveObdPackets, mqttPublish, obdDb, publicKeyPEM, emit, startT, s); err != nil && ctx.Err() == nil {
		emit(SimEvent{Elapsed: elap(), Tag: "PHASE 1", Cls: "er", Msg: fmt.Sprintf("Phase 1 error: %v", err), Ty: "warn"})
	}
	obdDb.Close()

	emit(SimEvent{
		Elapsed: elap(),
		Tag:     "PHASE 1",
		Cls:     "ph",
		Msg:     fmt.Sprintf("=== Phase 1 complete in %.1fs ===", float64(elap())/1000.0),
		Ty:      "ok",
		Step:    "phase1:done",
		Data:    map[string]interface{}{"elapsed": elap()},
	})

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

	if err := runPhase2(ctx, cfg, obdDbPath, livePkts, mqttPublish, emit, startT, s); err != nil && ctx.Err() == nil {
		emit(SimEvent{Elapsed: elap(), Tag: "PHASE 2", Cls: "er", Msg: fmt.Sprintf("Phase 2 error: %v", err), Ty: "warn"})
	}

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

// runPhase1 runs the three concurrent streams:
// A: sequential historic batch uploads
// B: GPS L1 MQTT streaming (cycles until A is done)
// C: OBD accumulation into SQLite (interval-based until A is done)
func runPhase1(ctx context.Context, cfg Config, batchFiles []string, liveGpsPackets []map[string]interface{}, liveObdPackets []map[string]interface{}, publish func(string, []byte) error, obdDb *sql.DB, publicKeyPEM string, emit func(SimEvent), startT time.Time, s *Simulator) error {
	elap := func() int64 { return time.Since(startT).Milliseconds() }
	phaseStart := time.Now()
	topic := cfg.TgtIMEI + "/obd"

	batchUploadDelay := time.Duration(cfg.BatchUploadDelayMs) * time.Millisecond
	if batchUploadDelay <= 0 {
		batchUploadDelay = 120 * time.Second
	}
	gpsInterval := time.Duration(cfg.GPSl1IntervalMs) * time.Millisecond
	if gpsInterval <= 0 {
		gpsInterval = 10 * time.Second
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

			if cfg.DryRun {
				emit(SimEvent{
					Elapsed: elap(), Tag: "DRY/UPLOAD", Cls: "up",
					Msg: fmt.Sprintf("SKIP upload: batch_%d (dry run)", i+1), Ty: "info",
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
						Msg:  fmt.Sprintf("OK 200 — batch_%d in %dms", i+1, elapsed.Milliseconds()),
						Ty:   "ok",
						Step: "p1:upload",
						Data: map[string]interface{}{"current": i + 1, "total": len(batchFiles), "n": i + 1},
					})
				}
			}

			emit(SimEvent{
				Elapsed: elap(), Tag: "UPLOAD", Cls: "up",
				Msg:  fmt.Sprintf("Batch %d/%d uploaded", i+1, len(batchFiles)),
				Ty:   "ok",
				Step: "p1:upload",
				Data: map[string]interface{}{"current": i + 1, "total": len(batchFiles), "n": i + 1},
			})

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

	// ── Stream B: GPS L1 MQTT ───────────────────────────────────────────────
	wg.Add(1)
	go func() {
		defer wg.Done()
		if len(liveGpsPackets) == 0 {
			<-phase1Done
			return
		}

		gpsIdx := 0
		published := 0

	gpsLoop:
		for {
			// Non-blocking check before sleeping
			select {
			case <-ctx.Done():
				break gpsLoop
			case <-phase1Done:
				break gpsLoop
			default:
			}
			// Block if paused (respects Stop immediately via ctx)
			if err := s.checkPause(ctx); err != nil {
				break gpsLoop
			}
			// Wait the GPS interval; time.After avoids accumulated ticks during pause
			select {
			case <-ctx.Done():
				break gpsLoop
			case <-phase1Done:
				break gpsLoop
			case <-time.After(gpsInterval):
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
			if err := publish(topic, data); err != nil {
				emit(SimEvent{Elapsed: elap(), Tag: "P1/GPS", Cls: "er", Msg: fmt.Sprintf("publish error: %v", err), Ty: "warn"})
			} else {
				published++
				emit(SimEvent{
					Elapsed: elap(), Tag: "P1/GPS", Cls: "mq",
					Msg:  fmt.Sprintf("GPS #%d → %s", published, topic),
					Ty:   "info", Step: "p1:gps",
					Data: map[string]interface{}{
						"published": published,
						"total":     len(liveGpsPackets),
						"payload":   string(data),
					},
				})
			}
		}
		emit(SimEvent{
			Elapsed: elap(), Tag: "P1/GPS", Cls: "mq",
			Msg:  fmt.Sprintf("GPS L1 stream stopped — total published: %d", published),
			Ty:   "info", Step: "p1:gps",
			Data: map[string]interface{}{"published": published, "total": len(liveGpsPackets)},
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

		obdIdx := 0
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

	phaseElapsed := time.Since(phaseStart).Milliseconds()
	_ = phaseElapsed
	return nil
}

// runPhase2 runs the two sequential steps:
// 1. Upload the accumulated OBD database
// 2. Stream all live packets via MQTT at normal interval
func runPhase2(ctx context.Context, cfg Config, obdDbPath string, livePkts []livePacket, publish func(string, []byte) error, emit func(SimEvent), startT time.Time, s *Simulator) error {
	elap := func() int64 { return time.Since(startT).Milliseconds() }
	topic := cfg.TgtIMEI + "/obd"

	// 1. Upload OBD DB
	if cfg.DryRun {
		emit(SimEvent{
			Elapsed: elap(), Tag: "DRY/UPLOAD", Cls: "up",
			Msg: fmt.Sprintf("SKIP upload: live_obd_%s.db (dry run)", cfg.TgtIMEI), Ty: "info",
		})
		time.Sleep(200 * time.Millisecond)
	} else {
		t0 := time.Now()
		if err := uploadFile(obdDbPath, cfg); err != nil {
			emit(SimEvent{
				Elapsed: elap(), Tag: "UPLOAD", Cls: "er",
				Msg: fmt.Sprintf("OBD upload FAILED: %v", err), Ty: "warn",
			})
		} else {
			emit(SimEvent{
				Elapsed: elap(), Tag: "UPLOAD", Cls: "up",
				Msg: fmt.Sprintf("OK 200 — live_obd_%s.db in %dms", cfg.TgtIMEI, time.Since(t0).Milliseconds()),
				Ty: "ok",
			})
		}
	}
	emit(SimEvent{
		Elapsed: elap(), Tag: "P2/UPLOAD", Cls: "up",
		Msg: "OBD DB upload complete", Ty: "ok", Step: "p2:upload:done",
	})

	if ctx.Err() != nil {
		return nil
	}

	// 2. Normal mode streaming
	normalInterval := time.Duration(cfg.NormalIntervalMs) * time.Millisecond
	if normalInterval <= 0 {
		normalInterval = 60 * time.Second
	}

	total := len(livePkts)
	gpsCount := 0
	obdCount := 0

	for i, lp := range livePkts {
		if ctx.Err() != nil {
			return nil
		}
		if err := s.checkPause(ctx); err != nil {
			return nil
		}

		data, _ := json.Marshal(lp.Packet)
		if cfg.DryRun {
			if i%20 == 0 {
				emit(SimEvent{
					Elapsed: elap(), Tag: "DRY/P2", Cls: "fe",
					Msg: fmt.Sprintf("SKIP publish #%d (dry run)", i+1), Ty: "info",
				})
			}
		} else {
			if err := publish(topic, data); err != nil {
				emit(SimEvent{Elapsed: elap(), Tag: "P2/STREAM", Cls: "er", Msg: fmt.Sprintf("publish error: %v", err), Ty: "warn"})
			} else {
				if lp.Type == "gps" {
					gpsCount++
				} else if lp.Type == "obd" {
					obdCount++
				}
			}
		}

		if (i+1)%5 == 0 || i+1 == 1 || i+1 == total {
			emit(SimEvent{
				Elapsed: elap(), Tag: "P2/STREAM", Cls: "fe",
				Msg:  fmt.Sprintf("Progress: %d/%d (gps:%d obd:%d)", i+1, total, gpsCount, obdCount),
				Ty:   "info",
				Step: "p2:stream",
				Data: map[string]interface{}{"current": i + 1, "total": total, "gps": gpsCount, "obd": obdCount},
			})
		}

		if i < total-1 {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(normalInterval):
			}
		}
	}

	emit(SimEvent{
		Elapsed: elap(), Tag: "P2/STREAM", Cls: "fe",
		Msg:  fmt.Sprintf("Normal mode complete — GPS: %d, OBD: %d", gpsCount, obdCount),
		Ty:   "ok",
		Step: "p2:stream",
		Data: map[string]interface{}{"current": total, "total": total, "gps": gpsCount, "obd": obdCount},
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

