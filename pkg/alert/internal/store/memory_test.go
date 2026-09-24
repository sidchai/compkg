package store

import (
	"testing"
	"time"
)

func TestMemoryStoreTTLCapacity(t *testing.T) {
	st := NewMemoryStore(2)
	ctx := t.Context()
	if err := st.Set(ctx, "a", "1", 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := st.Set(ctx, "b", "2", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := st.Set(ctx, "c", "3", time.Minute); err == nil {
		t.Fatal("want capacity error")
	}
	time.Sleep(30 * time.Millisecond)
	if st.Size() != 1 {
		t.Fatalf("ttl size=%d", st.Size())
	}
}
