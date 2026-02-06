/*
Copyright 2025 The Scion Authors.
*/

package handlers

import (
	"context"
	"sync"
	"time"

	"github.com/ptone/scion-agent/pkg/sciontool/hooks"
	"github.com/ptone/scion-agent/pkg/sciontool/hooks/session"
	"github.com/ptone/scion-agent/pkg/sciontool/log"
	"github.com/ptone/scion-agent/pkg/sciontool/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// SpanMapping defines how hook events map to span names.
var SpanMapping = map[string]string{
	hooks.EventSessionStart:    "agent.session.start",
	hooks.EventSessionEnd:      "agent.session.end",
	hooks.EventToolStart:       "agent.tool.call",
	hooks.EventToolEnd:         "agent.tool.result",
	hooks.EventPromptSubmit:    "agent.user.prompt",
	hooks.EventModelStart:      "gen_ai.api.request",
	hooks.EventModelEnd:        "gen_ai.api.response",
	hooks.EventAgentStart:      "agent.turn.start",
	hooks.EventAgentEnd:        "agent.turn.end",
	hooks.EventNotification:    "agent.notification",
	hooks.EventResponseComplete: "agent.response.complete",
	hooks.EventPreStart:        "agent.lifecycle.pre_start",
	hooks.EventPostStart:       "agent.lifecycle.post_start",
	hooks.EventPreStop:         "agent.lifecycle.pre_stop",
}

// inProgressSpan tracks a span that has been started but not ended.
type inProgressSpan struct {
	span      trace.Span
	ctx       context.Context
	startTime time.Time
	toolName  string
}

// TelemetryHandler converts hook events to OTLP spans.
type TelemetryHandler struct {
	tracer     trace.Tracer
	redactor   *telemetry.Redactor
	spanStore  sync.Map // map[string]*inProgressSpan - keyed by spanKey
	sessionDir string   // directory for session files (empty = default)
}

// NewTelemetryHandler creates a new telemetry handler.
// If tp is nil, a noop tracer will be used.
func NewTelemetryHandler(tp trace.TracerProvider, redactor *telemetry.Redactor) *TelemetryHandler {
	var tracer trace.Tracer
	if tp != nil {
		tracer = tp.Tracer("github.com/ptone/scion-agent/pkg/sciontool/hooks/handlers")
	} else {
		tracer = trace.NewNoopTracerProvider().Tracer("noop")
	}

	return &TelemetryHandler{
		tracer:   tracer,
		redactor: redactor,
	}
}

// Handle processes a hook event and emits a corresponding span.
func (h *TelemetryHandler) Handle(event *hooks.Event) error {
	if h == nil || event == nil {
		return nil
	}

	spanName, ok := SpanMapping[event.Name]
	if !ok {
		// Unknown event type - skip
		return nil
	}

	// Handle start/end pairing for tool calls and model calls
	switch event.Name {
	case hooks.EventToolStart:
		h.startSpan(event, spanName)
	case hooks.EventToolEnd:
		h.endSpan(event, spanName, hooks.EventToolStart)
	case hooks.EventModelStart:
		h.startSpan(event, spanName)
	case hooks.EventModelEnd:
		h.endSpan(event, spanName, hooks.EventModelStart)
	case hooks.EventAgentStart:
		h.startSpan(event, spanName)
	case hooks.EventAgentEnd:
		h.endSpan(event, spanName, hooks.EventAgentStart)
	default:
		// Single-shot events - create and immediately end span
		h.singleSpan(event, spanName)
	}

	return nil
}

// spanKey generates a unique key for tracking in-progress spans.
// For tool calls, we include the tool name to handle concurrent tool calls.
func (h *TelemetryHandler) spanKey(eventType, toolName string) string {
	if toolName != "" {
		return eventType + ":" + toolName
	}
	return eventType
}

// startSpan creates a new in-progress span.
func (h *TelemetryHandler) startSpan(event *hooks.Event, spanName string) {
	ctx := context.Background()
	attrs := h.eventToAttributes(event)

	ctx, span := h.tracer.Start(ctx, spanName, trace.WithAttributes(attrs...))

	key := h.spanKey(event.Name, event.Data.ToolName)
	h.spanStore.Store(key, &inProgressSpan{
		span:      span,
		ctx:       ctx,
		startTime: time.Now(),
		toolName:  event.Data.ToolName,
	})
}

// endSpan ends an in-progress span.
func (h *TelemetryHandler) endSpan(event *hooks.Event, spanName, startEventType string) {
	key := h.spanKey(startEventType, event.Data.ToolName)

	val, ok := h.spanStore.LoadAndDelete(key)
	if !ok {
		// No matching start event - create a single span
		h.singleSpan(event, spanName)
		return
	}

	inProgress := val.(*inProgressSpan)

	// Add end-event attributes
	attrs := h.eventToEndAttributes(event, inProgress.startTime)
	inProgress.span.SetAttributes(attrs...)

	// Set status based on success/error
	if event.Data.Error != "" {
		inProgress.span.SetStatus(codes.Error, event.Data.Error)
	} else if event.Data.Success {
		inProgress.span.SetStatus(codes.Ok, "")
	}

	inProgress.span.End()
}

// singleSpan creates and immediately ends a span.
func (h *TelemetryHandler) singleSpan(event *hooks.Event, spanName string) {
	ctx := context.Background()
	attrs := h.eventToAttributes(event)

	// For session-end events, try to add session metrics
	if event.Name == hooks.EventSessionEnd {
		sessionAttrs := h.getSessionMetricsAttributes()
		attrs = append(attrs, sessionAttrs...)
	}

	_, span := h.tracer.Start(ctx, spanName, trace.WithAttributes(attrs...))

	// Set status based on success/error
	if event.Data.Error != "" {
		span.SetStatus(codes.Error, event.Data.Error)
	} else if event.Data.Success {
		span.SetStatus(codes.Ok, "")
	}

	span.End()
}

// getSessionMetricsAttributes parses the latest session file and returns attributes.
func (h *TelemetryHandler) getSessionMetricsAttributes() []attribute.KeyValue {
	metrics, err := session.ParseLatestSession(h.sessionDir)
	if err != nil {
		log.Debug("Failed to parse session metrics: %v", err)
		return nil
	}

	attrs := []attribute.KeyValue{
		attribute.Int("tokens_input", metrics.TokensInput),
		attribute.Int("tokens_output", metrics.TokensOutput),
		attribute.Int("tokens_cached", metrics.TokensCached),
		attribute.Int("turn_count", metrics.TurnCount),
		attribute.Int64("duration_ms", metrics.Duration.Milliseconds()),
	}

	if metrics.Model != "" {
		attrs = append(attrs, attribute.String("model", metrics.Model))
	}

	// Add tool call statistics
	for toolName, stats := range metrics.ToolCalls {
		prefix := "tool." + toolName + "."
		attrs = append(attrs,
			attribute.Int(prefix+"calls", stats.Calls),
			attribute.Int(prefix+"success", stats.Success),
			attribute.Int(prefix+"errors", stats.Errors),
		)
	}

	return attrs
}

// eventToAttributes converts event data to span attributes.
func (h *TelemetryHandler) eventToAttributes(event *hooks.Event) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("event.name", event.Name),
	}

	if event.RawName != "" {
		attrs = append(attrs, attribute.String("event.raw_name", event.RawName))
	}
	if event.Dialect != "" {
		attrs = append(attrs, attribute.String("event.dialect", event.Dialect))
	}

	// Add data fields with redaction
	if event.Data.SessionID != "" {
		val := event.Data.SessionID
		if h.redactor != nil && h.redactor.ShouldHash("session_id") {
			val = telemetry.HashValue(val)
		}
		attrs = append(attrs, attribute.String("session_id", val))
	}

	if event.Data.ToolName != "" {
		attrs = append(attrs, attribute.String("tool_name", event.Data.ToolName))
	}

	if event.Data.ToolInput != "" {
		val := event.Data.ToolInput
		if h.redactor != nil && h.redactor.ShouldRedact("tool_input") {
			val = "[REDACTED]"
		}
		attrs = append(attrs, attribute.String("tool_input", val))
	}

	if event.Data.ToolOutput != "" {
		val := event.Data.ToolOutput
		if h.redactor != nil && h.redactor.ShouldRedact("tool_output") {
			val = "[REDACTED]"
		}
		attrs = append(attrs, attribute.String("tool_output", val))
	}

	if event.Data.Prompt != "" {
		val := event.Data.Prompt
		if h.redactor != nil && h.redactor.ShouldRedact("prompt") {
			val = "[REDACTED]"
		}
		attrs = append(attrs, attribute.String("prompt", val))
	}

	if event.Data.Source != "" {
		attrs = append(attrs, attribute.String("source", event.Data.Source))
	}

	if event.Data.Reason != "" {
		attrs = append(attrs, attribute.String("reason", event.Data.Reason))
	}

	if event.Data.Message != "" {
		attrs = append(attrs, attribute.String("message", event.Data.Message))
	}

	return attrs
}

// eventToEndAttributes creates attributes specific to end events.
func (h *TelemetryHandler) eventToEndAttributes(event *hooks.Event, startTime time.Time) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.Int64("duration_ms", time.Since(startTime).Milliseconds()),
	}

	if event.Data.Success {
		attrs = append(attrs, attribute.Bool("success", true))
	}

	if event.Data.Error != "" {
		attrs = append(attrs, attribute.String("error", event.Data.Error))
	}

	// Add tool output for end events
	if event.Data.ToolOutput != "" {
		val := event.Data.ToolOutput
		if h.redactor != nil && h.redactor.ShouldRedact("tool_output") {
			val = "[REDACTED]"
		}
		attrs = append(attrs, attribute.String("tool_output", val))
	}

	return attrs
}

// Flush ends any in-progress spans. Called during shutdown.
func (h *TelemetryHandler) Flush() {
	h.spanStore.Range(func(key, value any) bool {
		if inProgress, ok := value.(*inProgressSpan); ok {
			inProgress.span.SetStatus(codes.Error, "span not properly ended - flushed during shutdown")
			inProgress.span.End()
		}
		h.spanStore.Delete(key)
		return true
	})
}
