package trace

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// 测试前确保 propagator 注册（Bootstrap 没跑时也要可用）。
func ensurePropagator() {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
}

func TestInjectExtractRoundTrip(t *testing.T) {
	ensurePropagator()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	defer tp.Shutdown(context.Background())
	otel.SetTracerProvider(tp)

	ctx, span := tp.Tracer("test").Start(context.Background(), "root")
	defer span.End()

	carrier := propagation.MapCarrier{}
	Inject(ctx, carrier)
	if carrier[HeaderTraceparent] == "" {
		t.Fatalf("traceparent not injected: %#v", carrier)
	}

	// 模拟另一进程从 carrier 还原
	gotCtx := Extract(context.Background(), carrier)
	gotSC := oteltrace.SpanContextFromContext(gotCtx)
	wantSC := oteltrace.SpanContextFromContext(ctx)
	if !gotSC.IsValid() || gotSC.TraceID() != wantSC.TraceID() {
		t.Fatalf("traceID mismatch: got=%s want=%s", gotSC.TraceID(), wantSC.TraceID())
	}
}

func TestInjectExtractEnvelope(t *testing.T) {
	ensurePropagator()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	defer tp.Shutdown(context.Background())
	otel.SetTracerProvider(tp)

	ctx, span := tp.Tracer("test").Start(context.Background(), "root")
	defer span.End()

	env := InjectEnvelope(ctx)
	if env.Traceparent == "" {
		t.Fatalf("envelope empty")
	}

	gotCtx := ExtractEnvelope(context.Background(), env)
	if TraceIDFromContext(gotCtx) != TraceIDFromContext(ctx) {
		t.Fatalf("traceID mismatch")
	}
}

func TestExtractEnvelopeEmpty(t *testing.T) {
	ensurePropagator()
	parent := context.Background()
	got := ExtractEnvelope(parent, TraceContextEnvelope{})
	if got != parent {
		t.Fatalf("empty envelope should return original ctx")
	}
}
