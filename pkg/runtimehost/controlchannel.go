package runtimehost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"time"

	"github.com/ptone/scion-agent/pkg/apiclient"
	"github.com/ptone/scion-agent/pkg/wsprotocol"
)

// ControlChannelConfig holds configuration for the control channel client.
type ControlChannelConfig struct {
	// HubEndpoint is the base URL of the Hub API.
	HubEndpoint string
	// HostID is the unique identifier for this runtime host.
	HostID string
	// SecretKey is the HMAC secret key for authentication.
	SecretKey []byte
	// Version is the runtime host version string.
	Version string
	// Groves is a list of grove IDs this host serves.
	Groves []string

	// ReconnectBackoff configuration
	ReconnectInitial    time.Duration
	ReconnectMax        time.Duration
	ReconnectMultiplier float64

	// Connection timeouts
	PingInterval time.Duration
	PongWait     time.Duration
	WriteWait    time.Duration

	// Debug enables verbose logging.
	Debug bool
}

// DefaultControlChannelConfig returns the default configuration.
func DefaultControlChannelConfig() ControlChannelConfig {
	return ControlChannelConfig{
		ReconnectInitial:    1 * time.Second,
		ReconnectMax:        60 * time.Second,
		ReconnectMultiplier: 2.0,
		PingInterval:        30 * time.Second,
		PongWait:            60 * time.Second,
		WriteWait:           10 * time.Second,
		Debug:               false,
	}
}

// ControlChannelClient manages the WebSocket connection to the Hub.
type ControlChannelClient struct {
	config   ControlChannelConfig
	conn     *wsprotocol.Connection
	handlers http.Handler // Reuse existing HTTP handlers
	streams  map[string]*StreamHandler
	streamMu sync.RWMutex

	// Connection state
	connected   bool
	sessionID   string
	connectedAt time.Time
	mu          sync.RWMutex

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// StreamHandler handles a multiplexed stream.
type StreamHandler struct {
	streamID   string
	streamType string
	agentID    string
	dataCh     chan []byte
	closeCh    chan struct{}
	closed     bool
	closeMu    sync.Mutex
}

// NewControlChannelClient creates a new control channel client.
func NewControlChannelClient(config ControlChannelConfig, handlers http.Handler) *ControlChannelClient {
	return &ControlChannelClient{
		config:   config,
		handlers: handlers,
		streams:  make(map[string]*StreamHandler),
	}
}

// Connect establishes the WebSocket connection to the Hub.
func (c *ControlChannelClient) Connect(ctx context.Context) error {
	c.mu.Lock()
	if c.connected {
		c.mu.Unlock()
		return nil
	}
	c.ctx, c.cancel = context.WithCancel(ctx)
	c.mu.Unlock()

	return c.connectWithBackoff()
}

// connectWithBackoff attempts to connect with exponential backoff.
func (c *ControlChannelClient) connectWithBackoff() error {
	backoff := c.config.ReconnectInitial
	if backoff == 0 {
		backoff = 1 * time.Second
	}

	for {
		select {
		case <-c.ctx.Done():
			return c.ctx.Err()
		default:
		}

		if err := c.doConnect(); err != nil {
			log.Printf("[Host:ControlChannel] Connection failed: %v, retrying in %v", err, backoff)

			select {
			case <-c.ctx.Done():
				return c.ctx.Err()
			case <-time.After(backoff):
			}

			// Increase backoff
			backoff = time.Duration(float64(backoff) * c.config.ReconnectMultiplier)
			if c.config.ReconnectMax > 0 && backoff > c.config.ReconnectMax {
				backoff = c.config.ReconnectMax
			}
			continue
		}

		// Successfully connected, run the message loop
		c.runMessageLoop()

		// Connection lost, try to reconnect
		if c.ctx.Err() == nil {
			log.Printf("[Host:ControlChannel] Connection lost, reconnecting...")
			backoff = c.config.ReconnectInitial
			if backoff == 0 {
				backoff = 1 * time.Second
			}
		}
	}
}

// doConnect performs the actual WebSocket connection.
func (c *ControlChannelClient) doConnect() error {
	// Build WebSocket URL
	wsURL, err := c.buildWebSocketURL()
	if err != nil {
		return fmt.Errorf("invalid hub endpoint: %w", err)
	}

	// Build signed headers for authentication
	headers, err := c.buildAuthHeaders()
	if err != nil {
		return fmt.Errorf("failed to build auth headers: %w", err)
	}

	// Connect
	conn, resp, err := wsprotocol.Dial(c.ctx, wsURL, headers)
	if err != nil {
		if resp != nil {
			return fmt.Errorf("websocket dial failed (status %d): %w", resp.StatusCode, err)
		}
		return fmt.Errorf("websocket dial failed: %w", err)
	}

	c.conn = conn
	c.mu.Lock()
	c.connected = true
	c.connectedAt = time.Now()
	c.mu.Unlock()

	// Send connect message
	connectMsg := wsprotocol.NewConnectMessage(c.config.HostID, c.config.Version, c.config.Groves)
	if err := conn.WriteJSON(connectMsg); err != nil {
		c.conn.Close()
		return fmt.Errorf("failed to send connect message: %w", err)
	}

	// Wait for connected response
	if err := c.waitForConnected(); err != nil {
		c.conn.Close()
		return fmt.Errorf("connection handshake failed: %w", err)
	}

	log.Printf("[Host:ControlChannel] Connected to Hub (session: %s)", c.sessionID)
	return nil
}

// buildWebSocketURL constructs the WebSocket URL from the Hub endpoint.
func (c *ControlChannelClient) buildWebSocketURL() (string, error) {
	u, err := url.Parse(c.config.HubEndpoint)
	if err != nil {
		return "", err
	}

	// Convert http(s) to ws(s)
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	case "wss", "ws":
		// Already WebSocket scheme
	default:
		u.Scheme = "ws"
	}

	u.Path = "/api/v1/runtime-hosts/connect"
	return u.String(), nil
}

// buildAuthHeaders creates the HMAC-signed headers for authentication.
func (c *ControlChannelClient) buildAuthHeaders() (http.Header, error) {
	headers := http.Header{}

	if len(c.config.SecretKey) == 0 {
		// No auth configured
		headers.Set("X-Scion-Host-ID", c.config.HostID)
		return headers, nil
	}

	// Build a dummy request for signing
	req, err := http.NewRequest("GET", c.config.HubEndpoint+"/api/v1/runtime-hosts/connect", nil)
	if err != nil {
		return nil, err
	}

	// Apply HMAC auth using the HMACAuth type
	hmacAuth := &apiclient.HMACAuth{
		HostID:    c.config.HostID,
		SecretKey: c.config.SecretKey,
	}
	if err := hmacAuth.ApplyAuth(req); err != nil {
		return nil, fmt.Errorf("failed to apply HMAC auth: %w", err)
	}

	// Copy the signed headers
	for key := range req.Header {
		headers.Set(key, req.Header.Get(key))
	}

	return headers, nil
}

// waitForConnected waits for the connected response from the Hub.
func (c *ControlChannelClient) waitForConnected() error {
	// Set read deadline for handshake
	if err := c.conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return err
	}

	_, data, err := c.conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("failed to read connected message: %w", err)
	}

	env, err := wsprotocol.ParseEnvelope(data)
	if err != nil {
		return fmt.Errorf("failed to parse message: %w", err)
	}

	if env.Type != wsprotocol.TypeConnected {
		return fmt.Errorf("expected connected message, got %s", env.Type)
	}

	var connected wsprotocol.ConnectedMessage
	if err := json.Unmarshal(data, &connected); err != nil {
		return fmt.Errorf("failed to parse connected message: %w", err)
	}

	c.sessionID = connected.SessionID

	// Update ping interval if specified by Hub
	if connected.PingIntervalMs > 0 {
		c.config.PingInterval = time.Duration(connected.PingIntervalMs) * time.Millisecond
	}

	// Clear read deadline
	return c.conn.SetReadDeadline(time.Time{})
}

// runMessageLoop processes incoming messages.
func (c *ControlChannelClient) runMessageLoop() {
	// Start ping ticker
	c.wg.Add(1)
	go c.pingLoop()

	// Set pong handler
	c.conn.SetPongHandler(func(appData string) error {
		return c.conn.SetReadDeadline(time.Now().Add(c.config.PongWait))
	})

	// Set initial read deadline
	if err := c.conn.SetReadDeadline(time.Now().Add(c.config.PongWait)); err != nil {
		log.Printf("[Host:ControlChannel] Failed to set read deadline: %v", err)
		return
	}

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		_, data, err := c.conn.ReadMessage()
		if err != nil {
			if wsprotocol.IsUnexpectedCloseError(err, wsprotocol.CloseGoingAway, wsprotocol.CloseNormalClosure) {
				log.Printf("[Host:ControlChannel] Read error: %v", err)
			}
			c.markDisconnected()
			return
		}

		if err := c.handleMessage(data); err != nil {
			log.Printf("[Host:ControlChannel] Message handling error: %v", err)
		}
	}
}

// pingLoop sends periodic pings to keep the connection alive.
func (c *ControlChannelClient) pingLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.mu.RLock()
			connected := c.connected
			c.mu.RUnlock()

			if !connected {
				return
			}

			if err := c.conn.WritePing(); err != nil {
				log.Printf("[Host:ControlChannel] Ping failed: %v", err)
				return
			}
		}
	}
}

// handleMessage processes a single incoming message.
func (c *ControlChannelClient) handleMessage(data []byte) error {
	env, err := wsprotocol.ParseEnvelope(data)
	if err != nil {
		return fmt.Errorf("failed to parse envelope: %w", err)
	}

	switch env.Type {
	case wsprotocol.TypeRequest:
		return c.handleRequest(data)
	case wsprotocol.TypeStreamOpen:
		return c.handleStreamOpen(data)
	case wsprotocol.TypeStream:
		return c.handleStreamData(data)
	case wsprotocol.TypeStreamClose:
		return c.handleStreamClose(data)
	case wsprotocol.TypePing:
		return c.conn.WriteJSON(wsprotocol.NewPongMessage())
	default:
		if c.config.Debug {
			log.Printf("[Host:ControlChannel] Unknown message type: %s", env.Type)
		}
		return nil
	}
}

// handleRequest processes a tunneled HTTP request.
func (c *ControlChannelClient) handleRequest(data []byte) error {
	var req wsprotocol.RequestEnvelope
	if err := json.Unmarshal(data, &req); err != nil {
		return fmt.Errorf("failed to parse request: %w", err)
	}

	if c.config.Debug {
		log.Printf("[Host:ControlChannel] Request: %s %s", req.Method, req.Path)
	}

	// Build HTTP request
	path := req.Path
	if req.Query != "" {
		path = path + "?" + req.Query
	}

	var body io.Reader
	if len(req.Body) > 0 {
		body = bytes.NewReader(req.Body)
	}

	httpReq := httptest.NewRequest(req.Method, path, body)
	for key, value := range req.Headers {
		httpReq.Header.Set(key, value)
	}

	// Execute through existing handlers
	w := httptest.NewRecorder()
	c.handlers.ServeHTTP(w, httpReq)

	// Build response envelope
	result := w.Result()
	respBody, _ := io.ReadAll(result.Body)
	result.Body.Close()

	headers := make(map[string]string)
	for key := range result.Header {
		headers[key] = result.Header.Get(key)
	}

	resp := wsprotocol.NewResponseEnvelope(req.RequestID, result.StatusCode, headers, respBody)

	// Send response
	return c.conn.WriteJSON(resp)
}

// handleStreamOpen processes a stream open request.
func (c *ControlChannelClient) handleStreamOpen(data []byte) error {
	var open wsprotocol.StreamOpenMessage
	if err := json.Unmarshal(data, &open); err != nil {
		return fmt.Errorf("failed to parse stream open: %w", err)
	}

	if c.config.Debug {
		log.Printf("[Host:ControlChannel] Stream open: %s (type: %s, agent: %s)",
			open.StreamID, open.StreamType, open.AgentID)
	}

	// Create stream handler
	handler := &StreamHandler{
		streamID:   open.StreamID,
		streamType: open.StreamType,
		agentID:    open.AgentID,
		dataCh:     make(chan []byte, 256),
		closeCh:    make(chan struct{}),
	}

	c.streamMu.Lock()
	c.streams[open.StreamID] = handler
	c.streamMu.Unlock()

	// Start stream handler based on type
	switch open.StreamType {
	case wsprotocol.StreamTypePTY:
		go c.handlePTYStream(handler, open.Cols, open.Rows)
	default:
		log.Printf("[Host:ControlChannel] Unknown stream type: %s", open.StreamType)
	}

	return nil
}

// handleStreamData processes incoming stream data.
func (c *ControlChannelClient) handleStreamData(data []byte) error {
	var frame wsprotocol.StreamFrame
	if err := json.Unmarshal(data, &frame); err != nil {
		return fmt.Errorf("failed to parse stream frame: %w", err)
	}

	c.streamMu.RLock()
	handler, ok := c.streams[frame.StreamID]
	c.streamMu.RUnlock()

	if !ok {
		if c.config.Debug {
			log.Printf("[Host:ControlChannel] Data for unknown stream: %s", frame.StreamID)
		}
		return nil
	}

	select {
	case handler.dataCh <- frame.Data:
	default:
		log.Printf("[Host:ControlChannel] Stream buffer full: %s", frame.StreamID)
	}

	return nil
}

// handleStreamClose processes a stream close message.
func (c *ControlChannelClient) handleStreamClose(data []byte) error {
	var closeMsg wsprotocol.StreamCloseMessage
	if err := json.Unmarshal(data, &closeMsg); err != nil {
		return fmt.Errorf("failed to parse stream close: %w", err)
	}

	c.streamMu.Lock()
	handler, ok := c.streams[closeMsg.StreamID]
	if ok {
		delete(c.streams, closeMsg.StreamID)
	}
	c.streamMu.Unlock()

	if handler != nil {
		handler.closeMu.Lock()
		if !handler.closed {
			handler.closed = true
			close(handler.closeCh)
		}
		handler.closeMu.Unlock()
	}

	if c.config.Debug {
		log.Printf("[Host:ControlChannel] Stream closed: %s (reason: %s)", closeMsg.StreamID, closeMsg.Reason)
	}

	return nil
}

// handlePTYStream handles a PTY stream.
// This is a placeholder that will be fully implemented in Phase 5.
func (c *ControlChannelClient) handlePTYStream(handler *StreamHandler, cols, rows int) {
	log.Printf("[Host:ControlChannel] PTY stream started for agent %s (cols: %d, rows: %d)",
		handler.agentID, cols, rows)

	// PTY implementation will be added in Phase 5
	// For now, just wait for close
	<-handler.closeCh

	log.Printf("[Host:ControlChannel] PTY stream ended for agent %s", handler.agentID)
}

// SendStreamData sends data on a stream.
func (c *ControlChannelClient) SendStreamData(streamID string, data []byte) error {
	c.mu.RLock()
	connected := c.connected
	c.mu.RUnlock()

	if !connected {
		return fmt.Errorf("not connected")
	}

	frame := wsprotocol.NewStreamFrame(streamID, data)
	return c.conn.WriteJSON(frame)
}

// CloseStream closes a stream.
func (c *ControlChannelClient) CloseStream(streamID, reason string, code int) error {
	c.streamMu.Lock()
	handler, ok := c.streams[streamID]
	if ok {
		delete(c.streams, streamID)
	}
	c.streamMu.Unlock()

	if handler != nil {
		handler.closeMu.Lock()
		if !handler.closed {
			handler.closed = true
			close(handler.closeCh)
		}
		handler.closeMu.Unlock()
	}

	closeMsg := wsprotocol.NewStreamCloseMessage(streamID, reason, code)
	return c.conn.WriteJSON(closeMsg)
}

// markDisconnected updates the connection state.
func (c *ControlChannelClient) markDisconnected() {
	c.mu.Lock()
	c.connected = false
	c.mu.Unlock()

	// Close all streams
	c.streamMu.Lock()
	for _, handler := range c.streams {
		handler.closeMu.Lock()
		if !handler.closed {
			handler.closed = true
			close(handler.closeCh)
		}
		handler.closeMu.Unlock()
	}
	c.streams = make(map[string]*StreamHandler)
	c.streamMu.Unlock()
}

// Close closes the control channel connection.
func (c *ControlChannelClient) Close() error {
	c.mu.Lock()
	if c.cancel != nil {
		c.cancel()
	}
	c.mu.Unlock()

	c.wg.Wait()

	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// IsConnected returns whether the control channel is connected.
func (c *ControlChannelClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// SessionID returns the current session ID.
func (c *ControlChannelClient) SessionID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sessionID
}
