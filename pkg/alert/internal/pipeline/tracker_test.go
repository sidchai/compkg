package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/sidchai/compkg/pkg/alert/internal/core"
	"github.com/sidchai/compkg/pkg/alert/internal/store"
)

func TestPipelineMarkEndClearsTimeout(t *testing.T) {
	st := store.NewMemoryStore(100)
	p := New(st, nil)
	p.Now = func() time.Time { return time.Unix(1000, 0) }
	ctx := context.Background()
	if err := p.MarkStart(ctx, core.StagePlatformReady, "rec-1"); err != nil {
		t.Fatal(err)
	}
	if err := p.MarkEnd(ctx, core.StagePlatformReady, "rec-1"); err != nil {
		t.Fatal(err)
	}
	items := p.ScanTimeouts(ctx, time.Unix(2000, 0), func(ctx context.Context, item core.PendingItem) (bool, error) {
		t.Fatal("should not confirm ended mark")
		return false, nil
	})
	if len(items) != 0 {
		t.Fatalf("got %d", len(items))
	}
}

func TestPipelineTimeoutConfirmFinishedNoAlert(t *testing.T) {
	st := store.NewMemoryStore(100)
	p := New(st, nil)
	p.Now = func() time.Time { return time.Unix(1000, 0) }
	ctx := context.Background()
	_ = p.MarkStart(ctx, core.StagePlatformReady, "rec-2")
	items := p.ScanTimeouts(ctx, time.Unix(2000, 0), func(ctx context.Context, item core.PendingItem) (bool, error) {
		return true, nil
	})
	if len(items) != 0 {
		t.Fatalf("finished should not alert, got %d", len(items))
	}
}

func TestPipelineTimeoutStillOpen(t *testing.T) {
	st := store.NewMemoryStore(100)
	p := New(st, nil)
	p.Now = func() time.Time { return time.Unix(1000, 0) }
	ctx := context.Background()
	_ = p.MarkStart(ctx, core.StagePlatformReady, "rec-3")
	items := p.ScanTimeouts(ctx, time.Unix(2000, 0), func(ctx context.Context, item core.PendingItem) (bool, error) {
		return false, nil
	})
	if len(items) != 1 || items[0].BizKey != "rec-3" {
		t.Fatalf("%+v", items)
	}
}
