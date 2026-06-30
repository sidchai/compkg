package dispatch

import (
	"encoding/json"
	"testing"
)

// TestParseInstructionPayload_RejectLegacyNumeric 验证旧版纯数字 payload 不再被接受。
func TestParseInstructionPayload_RejectLegacyNumeric(t *testing.T) {
	if _, _, err := ParseInstructionPayload("12345"); err == nil {
		t.Fatalf("legacy numeric payload must be rejected")
	}
}

// TestParseInstructionPayload_EnvelopeWithTrace 验证新版 envelope 能解析出 queueId 和 trace carrier。
func TestParseInstructionPayload_EnvelopeWithTrace(t *testing.T) {
	raw, _ := json.Marshal(InstructionPayload{
		Schema:  InstructionPayloadSchema,
		QueueId: 999,
		Trace: map[string]string{
			"traceparent": "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01",
			"tracestate":  "rojo=00f067aa0ba902b7",
		},
	})
	queueId, carrier, err := ParseInstructionPayload(string(raw))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if queueId != 999 {
		t.Fatalf("queueId = %d, want 999", queueId)
	}
	if got := carrier["traceparent"]; got != "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01" {
		t.Fatalf("traceparent mismatch: %s", got)
	}
	if got := carrier["tracestate"]; got != "rojo=00f067aa0ba902b7" {
		t.Fatalf("tracestate mismatch: %s", got)
	}
}

// TestParseInstructionPayload_EnvelopeWithoutTrace 验证 envelope 无 trace 字段时 carrier 为 nil。
func TestParseInstructionPayload_EnvelopeWithoutTrace(t *testing.T) {
	raw, _ := json.Marshal(InstructionPayload{
		Schema:  InstructionPayloadSchema,
		QueueId: 42,
	})
	queueId, carrier, err := ParseInstructionPayload(string(raw))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if queueId != 42 {
		t.Fatalf("queueId = %d, want 42", queueId)
	}
	if carrier != nil {
		t.Fatalf("nil trace expected, got %v", carrier)
	}
}

// TestParseInstructionPayload_InvalidEnvelopeQueueId 验证 envelope 中 queueId 非法时报错。
func TestParseInstructionPayload_InvalidEnvelopeQueueId(t *testing.T) {
	if _, _, err := ParseInstructionPayload(`{"_s":"queue.v1","queueId":0}`); err == nil {
		t.Fatalf("expected error for queueId=0")
	}
}

// TestParseInstructionPayload_Empty 验证空 payload 报错。
func TestParseInstructionPayload_Empty(t *testing.T) {
	if _, _, err := ParseInstructionPayload("   "); err == nil {
		t.Fatalf("expected error for empty payload")
	}
}

// TestParseInstructionPayload_InvalidNonJson 验证非 JSON payload 报错。
func TestParseInstructionPayload_InvalidNonJson(t *testing.T) {
	if _, _, err := ParseInstructionPayload("abc"); err == nil {
		t.Fatalf("expected error for non-json payload")
	}
}
