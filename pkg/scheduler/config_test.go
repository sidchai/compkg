package scheduler

import (
	"context"
	"testing"
	"time"
)

func TestConfig_ApplyDefaults(t *testing.T) {
	c := Config{}
	c.applyDefaults()

	if c.MaxConcurrency != 50 {
		t.Errorf("MaxConcurrency default got=%d want=50", c.MaxConcurrency)
	}
	if c.DialTimeout != 5*time.Second {
		t.Errorf("DialTimeout default got=%s want=5s", c.DialTimeout)
	}
	if c.HeartbeatInterval != 5*time.Second {
		t.Errorf("HeartbeatInterval default got=%s want=5s", c.HeartbeatInterval)
	}
	if c.ReconnectMinBackoff != 1*time.Second {
		t.Errorf("ReconnectMinBackoff default got=%s want=1s", c.ReconnectMinBackoff)
	}
	if c.ReconnectMaxBackoff != 30*time.Second {
		t.Errorf("ReconnectMaxBackoff default got=%s want=30s", c.ReconnectMaxBackoff)
	}
	if c.SubmitTimeout != 5*time.Second {
		t.Errorf("SubmitTimeout default got=%s want=5s", c.SubmitTimeout)
	}
	if c.LocalBufferDiskSpillMaxBytes != defaultDiskSpillMaxBytes {
		t.Errorf("LocalBufferDiskSpillMaxBytes default got=%d want=%d", c.LocalBufferDiskSpillMaxBytes, defaultDiskSpillMaxBytes)
	}
	if c.InstanceID == "" {
		t.Error("InstanceID should be auto-filled with hostname")
	}
	if c.WorkerID == "" {
		t.Error("WorkerID should be auto-generated from AppName + InstanceID + pid")
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"全字段ok", Config{Endpoint: "x:9090", AppName: "a", AppKey: "k", AppSecret: "s"}, false},
		{"缺Endpoint", Config{AppName: "a", AppKey: "k", AppSecret: "s"}, true},
		{"缺AppName", Config{Endpoint: "x:9090", AppKey: "k", AppSecret: "s"}, true},
		{"缺AppKey", Config{Endpoint: "x:9090", AppName: "a", AppSecret: "s"}, true},
		{"缺AppSecret", Config{Endpoint: "x:9090", AppName: "a", AppKey: "k"}, true},
		{
			"backoff顺序错",
			Config{Endpoint: "x:9090", AppName: "a", AppKey: "k", AppSecret: "s",
				ReconnectMinBackoff: 10 * time.Second, ReconnectMaxBackoff: 1 * time.Second},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := tt.cfg
			c.applyDefaults()
			err := c.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() err=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestNew_RegisterHandler(t *testing.T) {
	c, err := New(Config{Endpoint: "x:9090", AppName: "a", AppKey: "k", AppSecret: "s"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.RegisterHandler("job1", func(_ context.Context, _ *Job) (string, error) { return "", nil })
	c.RegisterHandler("job2", func(_ context.Context, _ *Job) (string, error) { return "", nil })

	names := c.handlerNames()
	if len(names) != 2 {
		t.Fatalf("handlerNames len got=%d want=2", len(names))
	}
	if c.lookupHandler("job1") == nil || c.lookupHandler("job2") == nil {
		t.Fatal("lookupHandler should find registered jobs")
	}
	if c.lookupHandler("missing") != nil {
		t.Fatal("lookupHandler should return nil for missing job")
	}
}
