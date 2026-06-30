package trace

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"go.opentelemetry.io/otel/attribute"
)

func newRecorder(t *testing.T) (*tracetest.SpanRecorder, *sdktrace.TracerProvider) {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(rec),
	)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return rec, tp
}

func TestSetAttributes_DenyAndHash(t *testing.T) {
	rec, tp := newRecorder(t)
	currentPolicy.Store(DefaultAttributePolicy())

	_, span := tp.Tracer("t").Start(context.Background(), "s")
	SetAttributes(span,
		attribute.String("device_no", "dev-1"),
		attribute.String("phone", "13800000000"),    // deny
		attribute.String("user_id", "u-secret"),     // hash
		attribute.String("authorization", "Bearer"), // deny
	)
	span.End()

	if len(rec.Ended()) != 1 {
		t.Fatalf("expected 1 span")
	}
	attrs := rec.Ended()[0].Attributes()
	got := map[string]string{}
	for _, kv := range attrs {
		got[string(kv.Key)] = kv.Value.AsString()
	}
	if got["device_no"] != "dev-1" {
		t.Fatalf("device_no should pass through: %v", got)
	}
	if _, ok := got["phone"]; ok {
		t.Fatalf("phone should be denied")
	}
	if _, ok := got["authorization"]; ok {
		t.Fatalf("authorization should be denied")
	}
	uid := got["user_id"]
	if uid == "" || uid == "u-secret" {
		t.Fatalf("user_id should be hashed, got %q", uid)
	}
	if len(uid) != 12 {
		t.Fatalf("hashed user_id length=%d want=12", len(uid))
	}
}

func TestSetBizAttributes_SkipsZero(t *testing.T) {
	rec, tp := newRecorder(t)
	currentPolicy.Store(DefaultAttributePolicy())

	_, span := tp.Tracer("t").Start(context.Background(), "s")
	SetBizAttributes(span, BizAttrs{
		DeviceNo:    "dev-1",
		AccountID:   "10001",
		TraceOrigin: OriginOpenAPI,
	})
	span.End()

	attrs := rec.Ended()[0].Attributes()
	keys := map[string]bool{}
	for _, kv := range attrs {
		keys[string(kv.Key)] = true
	}
	if !keys[AttrDeviceNo] || !keys[AttrAccountID] || !keys[AttrTraceOrigin] {
		t.Fatalf("missing required attrs: %v", keys)
	}
	if keys[AttrMsgID] || keys[AttrInstructionID] || keys[AttrTraceOrphan] {
		t.Fatalf("zero fields should be skipped: %v", keys)
	}
}
