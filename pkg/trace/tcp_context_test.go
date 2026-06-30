package trace

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
)

func setupTracer(t *testing.T) *sdktrace.TracerProvider {
	t.Helper()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return tp
}

func TestTCPContextMap_SaveLoadHit(t *testing.T) {
	tp := setupTracer(t)
	m := NewTCPContextMap(time.Minute)
	defer m.Close()

	ctx, span := tp.Tracer("t").Start(context.Background(), "tcp.send")
	defer span.End()

	m.Save(ctx, "msg-1", time.Minute, "dev-1", "record_start")
	got, deviceNo, insType, ok := m.Load(context.Background(), "msg-1")
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if deviceNo != "dev-1" || insType != "record_start" {
		t.Fatalf("metadata mismatch: %s %s", deviceNo, insType)
	}
	wantTID := oteltrace.SpanContextFromContext(ctx).TraceID()
	gotTID := oteltrace.SpanContextFromContext(got).TraceID()
	if wantTID != gotTID {
		t.Fatalf("traceID mismatch")
	}

	// Load 后应已删除
	if _, _, _, ok := m.Load(context.Background(), "msg-1"); ok {
		t.Fatalf("entry should be consumed once")
	}
}

func TestTCPContextMap_Miss(t *testing.T) {
	m := NewTCPContextMap(time.Minute)
	defer m.Close()
	if _, _, _, ok := m.Load(context.Background(), "nope"); ok {
		t.Fatalf("expected miss")
	}
}

func TestTCPContextMap_Expire(t *testing.T) {
	tp := setupTracer(t)
	m := NewTCPContextMap(time.Minute)
	defer m.Close()
	ctx, span := tp.Tracer("t").Start(context.Background(), "tcp.send")
	defer span.End()

	m.Save(ctx, "msg-1", 1*time.Millisecond, "d", "i")
	time.Sleep(20 * time.Millisecond)
	if _, _, _, ok := m.Load(context.Background(), "msg-1"); ok {
		t.Fatalf("expected expired miss")
	}
}

func TestTCPContextMap_InvalidSpanContext(t *testing.T) {
	m := NewTCPContextMap(time.Minute)
	defer m.Close()
	// ctx 没有 SpanContext，Save 后 Load 仍能拿到 metadata，但 ok=false
	m.Save(context.Background(), "msg-x", time.Minute, "d-x", "i-x")
	_, deviceNo, insType, ok := m.Load(context.Background(), "msg-x")
	if ok {
		t.Fatalf("invalid SpanContext should produce ok=false")
	}
	if deviceNo != "d-x" || insType != "i-x" {
		t.Fatalf("metadata should still be returned for diagnostics")
	}
}

func TestTCPContextMap_Stats(t *testing.T) {
	m := NewTCPContextMap(time.Minute)
	defer m.Close()
	m.Save(context.Background(), "a", time.Minute, "", "")
	m.Save(context.Background(), "b", time.Minute, "", "")
	if got := m.Stats(); got != 2 {
		t.Fatalf("stats=%d want=2", got)
	}
}
