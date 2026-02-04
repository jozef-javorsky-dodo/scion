// Package runtimehost provides the Scion Runtime Host API server.
package runtimehost

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/ptone/scion-agent/pkg/agent"
	"github.com/ptone/scion-agent/pkg/hubclient"
)

const (
	// DefaultHeartbeatInterval is the default interval between heartbeats.
	DefaultHeartbeatInterval = 30 * time.Second
	// MinHeartbeatInterval is the minimum allowed heartbeat interval.
	MinHeartbeatInterval = 5 * time.Second
)

// HeartbeatConfig configures the heartbeat service.
type HeartbeatConfig struct {
	// Interval is the time between heartbeats.
	Interval time.Duration
	// Enabled controls whether heartbeats are sent.
	Enabled bool
}

// DefaultHeartbeatConfig returns the default heartbeat configuration.
func DefaultHeartbeatConfig() HeartbeatConfig {
	return HeartbeatConfig{
		Interval: DefaultHeartbeatInterval,
		Enabled:  true,
	}
}

// HeartbeatService sends periodic heartbeats to the Hub.
type HeartbeatService struct {
	client   hubclient.RuntimeHostService
	hostID   string
	interval time.Duration
	manager  agent.Manager
	version  string

	mu     sync.Mutex
	stopCh chan struct{}
	doneCh chan struct{}
	logger *log.Logger
}

// NewHeartbeatService creates a new heartbeat service.
// The client must be an authenticated hubclient.RuntimeHostService.
// The manager is used to gather agent status information.
func NewHeartbeatService(client hubclient.RuntimeHostService, hostID string, interval time.Duration, manager agent.Manager) *HeartbeatService {
	if interval < MinHeartbeatInterval {
		interval = MinHeartbeatInterval
	}

	return &HeartbeatService{
		client:   client,
		hostID:   hostID,
		interval: interval,
		manager:  manager,
		logger:   log.Default(),
	}
}

// SetLogger sets the logger for the heartbeat service.
func (s *HeartbeatService) SetLogger(logger *log.Logger) {
	s.logger = logger
}

// SetVersion sets the host version reported in heartbeats.
func (s *HeartbeatService) SetVersion(version string) {
	s.version = version
}

// Start begins sending heartbeats in the background.
// It blocks until Stop is called or the context is cancelled.
// If already started, this is a no-op.
func (s *HeartbeatService) Start(ctx context.Context) {
	s.mu.Lock()
	if s.stopCh != nil {
		s.mu.Unlock()
		return // Already running
	}
	s.stopCh = make(chan struct{})
	s.doneCh = make(chan struct{})
	s.mu.Unlock()

	go s.run(ctx)
}

// Stop signals the heartbeat service to stop and waits for it to finish.
func (s *HeartbeatService) Stop() {
	s.mu.Lock()
	if s.stopCh == nil {
		s.mu.Unlock()
		return // Not running
	}
	close(s.stopCh)
	doneCh := s.doneCh
	s.mu.Unlock()

	// Wait for the run goroutine to finish
	<-doneCh

	s.mu.Lock()
	s.stopCh = nil
	s.doneCh = nil
	s.mu.Unlock()
}

// IsRunning returns true if the heartbeat service is currently running.
func (s *HeartbeatService) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopCh != nil
}

// run is the main heartbeat loop.
func (s *HeartbeatService) run(ctx context.Context) {
	defer close(s.doneCh)

	// Send initial heartbeat immediately
	if err := s.sendHeartbeat(ctx); err != nil {
		s.logger.Printf("[Host:Heartbeat] Initial heartbeat failed: %v", err)
	} else {
		s.logger.Printf("[Host:Heartbeat] Initial heartbeat sent to Hub")
	}

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := s.sendHeartbeat(ctx); err != nil {
				s.logger.Printf("[Host:Heartbeat] Failed to send heartbeat: %v", err)
			}
		case <-s.stopCh:
			s.logger.Printf("[Host:Heartbeat] Service stopping")
			return
		case <-ctx.Done():
			s.logger.Printf("[Host:Heartbeat] Context cancelled")
			return
		}
	}
}

// sendHeartbeat sends a single heartbeat to the Hub.
func (s *HeartbeatService) sendHeartbeat(ctx context.Context) error {
	heartbeat := s.buildHeartbeat()
	return s.client.Heartbeat(ctx, s.hostID, heartbeat)
}

// buildHeartbeat constructs the heartbeat payload from current state.
func (s *HeartbeatService) buildHeartbeat() *hubclient.HostHeartbeat {
	status := "online"

	heartbeat := &hubclient.HostHeartbeat{
		Status: status,
	}

	// If we have a manager, gather per-grove agent counts
	if s.manager != nil {
		groveAgents := s.gatherGroveAgents()
		if len(groveAgents) > 0 {
			heartbeat.Groves = groveAgents
		}
	}

	return heartbeat
}

// gatherGroveAgents collects agent information grouped by grove.
func (s *HeartbeatService) gatherGroveAgents() []hubclient.GroveHeartbeat {
	if s.manager == nil {
		return nil
	}

	// List all agents managed by this host
	agents, err := s.manager.List(context.Background(), nil)
	if err != nil {
		s.logger.Printf("[Host:Heartbeat] Failed to list agents: %v", err)
		return nil
	}

	// Group agents by grove
	groveMap := make(map[string][]hubclient.AgentHeartbeat)
	for _, ag := range agents {
		groveID := ag.GroveID
		if groveID == "" {
			groveID = ag.Grove
		}
		if groveID == "" {
			groveID = "default"
		}

		agentHB := hubclient.AgentHeartbeat{
			AgentID:         ag.Name,
			Status:          ag.SessionStatus,
			ContainerStatus: ag.ContainerStatus,
		}
		groveMap[groveID] = append(groveMap[groveID], agentHB)
	}

	// Convert to slice
	var groves []hubclient.GroveHeartbeat
	for groveID, agentList := range groveMap {
		groves = append(groves, hubclient.GroveHeartbeat{
			GroveID:    groveID,
			AgentCount: len(agentList),
			Agents:     agentList,
		})
	}

	return groves
}

// ForceHeartbeat sends an immediate heartbeat, bypassing the interval.
// This can be used when significant state changes occur.
func (s *HeartbeatService) ForceHeartbeat(ctx context.Context) error {
	return s.sendHeartbeat(ctx)
}
