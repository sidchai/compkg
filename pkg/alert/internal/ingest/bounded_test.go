package ingest

import (
	"testing"
	"time"

	"github.com/sidchai/compkg/pkg/alert/internal/core"
)

func TestIngestDrop(t *testing.T) {
	ing := New(1, 1, func(core.Sample) { time.Sleep(30 * time.Millisecond) })
	defer ing.Close()
	for i := 0; i < 20; i++ {
		ing.Ingest(core.Sample{Kind: "x"})
	}
	time.Sleep(5 * time.Millisecond)
	if ing.Dropped() == 0 {
		t.Fatal("want drops")
	}
}
