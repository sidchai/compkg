package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestSubmitBuffer_PushDrainPutBack(t *testing.T) {
	b := newSubmitBuffer(3)
	for i := 0; i < 3; i++ {
		if !b.push(bufferedTask{opts: SubmitOptions{JobName: "j"}}) {
			t.Fatalf("push %d should succeed", i)
		}
	}
	if b.push(bufferedTask{opts: SubmitOptions{JobName: "j"}}) {
		t.Fatal("push beyond cap should fail")
	}
	if got := b.Len(); got != 3 {
		t.Fatalf("Len=%d want 3", got)
	}
	out := b.drain(2)
	if len(out) != 2 || b.Len() != 1 {
		t.Fatalf("drain unexpected: out=%d remain=%d", len(out), b.Len())
	}
	b.putBack(out)
	if b.Len() != 3 {
		t.Fatalf("putBack expected 3, got %d", b.Len())
	}
}

func TestSubmitBuffer_PutBackOverCapDropsOldest(t *testing.T) {
	b := newSubmitBuffer(2)
	b.push(bufferedTask{opts: SubmitOptions{JobName: "a"}})
	// 模拟回插 3 条到容量 2 的队列：保留最新 2 条
	b.putBack([]bufferedTask{
		{opts: SubmitOptions{JobName: "x"}},
		{opts: SubmitOptions{JobName: "y"}},
		{opts: SubmitOptions{JobName: "z"}},
	})
	if b.Len() != 2 {
		t.Fatalf("expected cap=2, got %d", b.Len())
	}
}

func TestIsRetriableSubmitErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain", errors.New("dial tcp: connection refused"), true},
		{"unavailable", status.Error(codes.Unavailable, "x"), true},
		{"deadline", status.Error(codes.DeadlineExceeded, "x"), true},
		{"invalid", status.Error(codes.InvalidArgument, "x"), false},
		{"notfound", status.Error(codes.NotFound, "x"), false},
	}
	for _, c := range cases {
		if got := isRetriableSubmitErr(c.err); got != c.want {
			t.Errorf("%s: got=%v want=%v", c.name, got, c.want)
		}
	}
}

func TestSubmitSpool_PushDrainAckReplay(t *testing.T) {
	dir := t.TempDir()
	spool, err := newSubmitSpool(dir, 1024*1024)
	if err != nil {
		t.Fatalf("newSubmitSpool: %v", err)
	}
	for _, name := range []string{"job-a", "job-b"} {
		if err := spool.push(bufferedTask{
			opts:       SubmitOptions{JobName: name, BizKey: name, Payload: []byte(name)},
			enqueuedAt: time.Now(),
		}); err != nil {
			t.Fatalf("push %s: %v", name, err)
		}
	}
	if got := spool.len(); got != 2 {
		t.Fatalf("spool len=%d want 2", got)
	}
	reloaded, err := newSubmitSpool(dir, 1024*1024)
	if err != nil {
		t.Fatalf("reload spool: %v", err)
	}
	items, err := reloaded.drain(10)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if len(items) != 2 || items[0].Opts.JobName != "job-a" || items[1].Opts.JobName != "job-b" {
		t.Fatalf("unexpected replay order: %#v", items)
	}
	if err := reloaded.ack(items[0].Seq); err != nil {
		t.Fatalf("ack: %v", err)
	}
	if got := reloaded.len(); got != 1 {
		t.Fatalf("after ack len=%d want 1", got)
	}
}

func TestSubmitSpool_MaxBytesEvictsOldest(t *testing.T) {
	dir := t.TempDir()
	spool, err := newSubmitSpool(dir, 430)
	if err != nil {
		t.Fatalf("newSubmitSpool: %v", err)
	}
	for _, name := range []string{"job-a", "job-b", "job-c"} {
		if err := spool.push(bufferedTask{
			opts:       SubmitOptions{JobName: name, BizKey: name, Payload: []byte("payload")},
			enqueuedAt: time.Now(),
		}); err != nil {
			t.Fatalf("push %s: %v", name, err)
		}
	}
	items, err := spool.drain(10)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected at least one spooled item after eviction")
	}
	if items[0].Opts.JobName == "job-a" && len(items) > 1 {
		t.Fatalf("expected oldest job-a to be evicted, got %#v", items)
	}
	if spool.bytes() > 430 {
		t.Fatalf("spool bytes=%d exceeds limit", spool.bytes())
	}
}

func TestClient_EnqueueFallsBackToDiskSpillWhenMemoryFull(t *testing.T) {
	c, err := New(Config{
		Endpoint:                     "x:9090",
		AppName:                      "a",
		AppKey:                       "k",
		AppSecret:                    "s",
		LocalBufferEnabled:           true,
		LocalBufferCapacity:          1,
		LocalBufferDiskSpillEnabled:  true,
		LocalBufferDiskSpillDir:      t.TempDir(),
		LocalBufferDiskSpillMaxBytes: 1024 * 1024,
		LocalBufferRetryInterval:     time.Hour,
		LocalBufferRetryBatch:        10,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if queued, err := c.EnqueueTask(context.Background(), SubmitOptions{JobName: "job-a"}); err != nil || !queued {
		t.Fatalf("enqueue first queued=%v err=%v", queued, err)
	}
	if queued, err := c.EnqueueTask(context.Background(), SubmitOptions{JobName: "job-b"}); err != nil || !queued {
		t.Fatalf("enqueue spill queued=%v err=%v", queued, err)
	}
	if c.BufferedCount() != 1 || c.SpilledCount() != 1 {
		t.Fatalf("counts buffer=%d spill=%d want 1/1", c.BufferedCount(), c.SpilledCount())
	}
}
