package axiom

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/apex/log"
	"github.com/goccy/go-json"

	"github.com/Minenetpro/pelican-wings/config"
	"github.com/Minenetpro/pelican-wings/events"
	"github.com/Minenetpro/pelican-wings/server"
	"github.com/Minenetpro/pelican-wings/system"
)

// AxiomEvent is the unified event type sent to the Axiom ingest API.
type AxiomEvent struct {
	Time             string  `json:"_time"`
	EventType        string  `json:"event_type"`
	ServerID         string  `json:"server_id"`
	Status           string  `json:"status,omitempty"`
	Line             string  `json:"line,omitempty"`
	MemoryBytes      uint64  `json:"memory_bytes,omitempty"`
	MemoryLimitBytes uint64  `json:"memory_limit_bytes,omitempty"`
	CpuAbsolute      float64 `json:"cpu_absolute,omitempty"`
	NetworkRxBytes   uint64  `json:"network_rx_bytes,omitempty"`
	NetworkTxBytes   uint64  `json:"network_tx_bytes,omitempty"`
	Uptime           int64   `json:"uptime,omitempty"`
	DiskBytes        int64   `json:"disk_bytes,omitempty"`
	State            string  `json:"state,omitempty"`
}

// Ingestor subscribes to server events and console output, batches them, and
// POSTs them to the Axiom ingest API.
type Ingestor struct {
	cfg     config.AxiomConfiguration
	client  *http.Client
	eventCh chan AxiomEvent
	manager *server.Manager
	ctx     context.Context

	// subscribedMu protects subscribedServers from concurrent access.
	subscribedMu      sync.Mutex
	subscribedServers map[string]struct{}

	dropWarnMu   sync.Mutex
	lastDropWarn time.Time
}

// NewIngestor creates and starts a new Axiom Ingestor. It returns nil if the
// integration is disabled or misconfigured. All spawned goroutines are bound to
// ctx for clean shutdown.
func NewIngestor(ctx context.Context, manager *server.Manager) *Ingestor {
	cfg := config.Get().Axiom
	if !cfg.Enabled {
		return nil
	}
	if cfg.URL == "" || cfg.APIToken == "" || cfg.Dataset == "" {
		log.Warn("axiom: enabled but url, api_token, or dataset is empty; skipping initialization")
		return nil
	}

	ing := &Ingestor{
		cfg: cfg,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		eventCh:           make(chan AxiomEvent, 10000),
		manager:           manager,
		ctx:               ctx,
		subscribedServers: make(map[string]struct{}),
	}

	// Register the hook FIRST to ensure no servers are missed if one is added
	// between All() and hook registration. The trySubscribe method ensures
	// we never double-subscribe to the same server.
	manager.OnServerAdd(func(s *server.Server) {
		ing.trySubscribe(s)
	})

	// Subscribe to all existing servers.
	for _, s := range manager.All() {
		ing.trySubscribe(s)
	}

	go ing.flusher(ctx)

	return ing
}

// trySubscribe attempts to subscribe to a server's events. It returns false
// if the server is already subscribed (preventing duplicate subscriptions).
func (ing *Ingestor) trySubscribe(s *server.Server) bool {
	ing.subscribedMu.Lock()
	if _, exists := ing.subscribedServers[s.ID()]; exists {
		ing.subscribedMu.Unlock()
		return false
	}
	ing.subscribedServers[s.ID()] = struct{}{}
	ing.subscribedMu.Unlock()

	log.WithField("server", s.ID()).Debug("axiom: subscribing to server")
	go ing.subscribeServer(ing.ctx, s)
	return true
}

// subscribeServer listens to a single server's Events bus and LogSink and
// forwards decoded events into the shared event channel.
func (ing *Ingestor) subscribeServer(ctx context.Context, s *server.Server) {
	eventCh := make(chan []byte, 64)
	logCh := make(chan []byte, 64)

	s.Events().On(eventCh)
	s.Sink(system.LogSink).On(logCh)

	serverID := s.ID()

	defer func() {
		s.Events().Off(eventCh)
		s.Sink(system.LogSink).Off(logCh)

		// Remove from subscribed set so we can re-subscribe if the server
		// is recreated with the same ID.
		ing.subscribedMu.Lock()
		delete(ing.subscribedServers, serverID)
		ing.subscribedMu.Unlock()

		log.WithField("server", serverID).Debug("axiom: unsubscribed from server")
	}()

	for {
		select {
		case data, ok := <-eventCh:
			if !ok {
				return
			}
			ing.processEvent(serverID, data)
		case data, ok := <-logCh:
			if !ok {
				return
			}
			ing.processConsoleOutput(serverID, data)
		case <-s.Context().Done():
			return
		case <-ctx.Done():
			return
		}
	}
}

// statsEventData mirrors the structure published by server.Events().Publish(StatsEvent, ...).
type statsEventData struct {
	Topic string              `json:"topic"`
	Data  statsEventPayload   `json:"data"`
}

type statsEventPayload struct {
	Memory      uint64  `json:"memory_bytes"`
	MemoryLimit uint64  `json:"memory_limit_bytes"`
	CpuAbsolute float64 `json:"cpu_absolute"`
	Network     struct {
		RxBytes uint64 `json:"rx_bytes"`
		TxBytes uint64 `json:"tx_bytes"`
	} `json:"network"`
	Uptime int64  `json:"uptime"`
	Disk   int64  `json:"disk_bytes"`
	State  *struct {
		Value string `json:"value"`
	} `json:"state,omitempty"`
}

// statusEventData mirrors the structure published by server.Events().Publish(StatusEvent, ...).
type statusEventData struct {
	Topic string `json:"topic"`
	Data  string `json:"data"`
}

// processEvent decodes an Events bus message and enqueues the corresponding
// AxiomEvent. Only StatsEvent and StatusEvent are handled; all others
// (including ConsoleOutputEvent) are skipped to avoid duplication with LogSink.
func (ing *Ingestor) processEvent(serverID string, data []byte) {
	var e events.Event
	if err := events.DecodeTo(data, &e); err != nil {
		return
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	switch e.Topic {
	case server.StatsEvent:
		var stats statsEventData
		if err := events.DecodeTo(data, &stats); err != nil {
			log.WithField("error", err).Warn("axiom: failed to decode stats event")
			return
		}
		ev := AxiomEvent{
			Time:             now,
			EventType:        "stats",
			ServerID:         serverID,
			MemoryBytes:      stats.Data.Memory,
			MemoryLimitBytes: stats.Data.MemoryLimit,
			CpuAbsolute:      stats.Data.CpuAbsolute,
			NetworkRxBytes:   stats.Data.Network.RxBytes,
			NetworkTxBytes:   stats.Data.Network.TxBytes,
			Uptime:           stats.Data.Uptime,
			DiskBytes:        stats.Data.Disk,
		}
		if stats.Data.State != nil {
			ev.State = stats.Data.State.Value
		}
		ing.enqueue(ev)

	case server.StatusEvent:
		var status statusEventData
		if err := events.DecodeTo(data, &status); err != nil {
			log.WithField("error", err).Warn("axiom: failed to decode status event")
			return
		}
		ing.enqueue(AxiomEvent{
			Time:      now,
			EventType: "status",
			ServerID:  serverID,
			Status:    status.Data,
		})
	}
}

// processConsoleOutput converts a raw log line into a console_output AxiomEvent.
func (ing *Ingestor) processConsoleOutput(serverID string, data []byte) {
	ing.enqueue(AxiomEvent{
		Time:      time.Now().UTC().Format(time.RFC3339Nano),
		EventType: "console_output",
		ServerID:  serverID,
		Line:      string(data),
	})
}

// enqueue performs a non-blocking send to the event channel. If the channel is
// full the event is dropped and a rate-limited warning is logged.
func (ing *Ingestor) enqueue(ev AxiomEvent) {
	select {
	case ing.eventCh <- ev:
	default:
		ing.warnDrop()
	}
}

// warnDrop logs a warning at most once per minute when events are dropped.
func (ing *Ingestor) warnDrop() {
	ing.dropWarnMu.Lock()
	defer ing.dropWarnMu.Unlock()
	if time.Since(ing.lastDropWarn) >= time.Minute {
		log.Warn("axiom: event channel full, dropping events")
		ing.lastDropWarn = time.Now()
	}
}

// flusher is the background goroutine that accumulates events from eventCh and
// flushes them to Axiom either when the batch is full or the flush interval
// fires.
func (ing *Ingestor) flusher(ctx context.Context) {
	interval := time.Duration(ing.cfg.FlushInterval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	batchSize := ing.cfg.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}
	batch := make([]AxiomEvent, 0, batchSize)

	for {
		select {
		case ev := <-ing.eventCh:
			batch = append(batch, ev)
			if len(batch) >= batchSize {
				ing.flush(batch)
				batch = make([]AxiomEvent, 0, batchSize)
				ticker.Reset(interval)
			}
		case <-ticker.C:
			if len(batch) > 0 {
				ing.flush(batch)
				batch = make([]AxiomEvent, 0, batchSize)
			}
		case <-ctx.Done():
			// Drain remaining events.
			for {
				select {
				case ev := <-ing.eventCh:
					batch = append(batch, ev)
				default:
					goto done
				}
			}
		done:
			if len(batch) > 0 {
				ing.flush(batch)
			}
			log.Info("axiom: shutdown complete")
			return
		}
	}
}

// flush POSTs a batch of events to the Axiom ingest API with retry on 5xx /
// network errors. 4xx errors are treated as permanent failures.
func (ing *Ingestor) flush(batch []AxiomEvent) {
	body, err := json.Marshal(batch)
	if err != nil {
		log.WithField("error", err).Error("axiom: failed to marshal event batch")
		return
	}

	url := fmt.Sprintf("%s/v1/datasets/%s/ingest", ing.cfg.URL, ing.cfg.Dataset)
	reader := bytes.NewReader(body)

	backoffs := []time.Duration{0, 1 * time.Second, 2 * time.Second}
	for attempt, backoff := range backoffs {
		if backoff > 0 {
			time.Sleep(backoff)
		}

		reader.Seek(0, 0)
		req, err := http.NewRequest(http.MethodPost, url, reader)
		if err != nil {
			log.WithField("error", err).Error("axiom: failed to create HTTP request")
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+ing.cfg.APIToken)

		resp, err := ing.client.Do(req)
		if err != nil {
			log.WithFields(log.Fields{
				"error":   err,
				"attempt": attempt + 1,
			}).Warn("axiom: network error during ingest")
			continue
		}
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return
		}

		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			log.WithFields(log.Fields{
				"status":  resp.StatusCode,
				"events":  len(batch),
			}).Error("axiom: permanent ingest failure (4xx), not retrying")
			return
		}

		log.WithFields(log.Fields{
			"status":  resp.StatusCode,
			"attempt": attempt + 1,
		}).Warn("axiom: server error during ingest, retrying")
	}

	log.WithField("events", len(batch)).Error("axiom: failed to ingest batch after all retries")
}
