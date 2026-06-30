package main

import (
	"testing"
	"time"
)

// TestJudgeService 覆盖发版判定的核心三维比对：实例数 / commit 一致 / 重启时间。
func TestJudgeService(t *testing.T) {
	deployTime := time.Date(2026, 6, 30, 14, 0, 0, 0, time.UTC)
	afterDeploy := deployTime.Add(time.Minute)   // 发版后启动（正常）
	beforeDeploy := deployTime.Add(-time.Minute) // 发版前启动（未重启）

	mk := func(alive, commitMatch, restarted bool, start time.Time) instanceResult {
		return instanceResult{
			Instance: "host-1-abcd", Alive: alive,
			CommitMatch: commitMatch, RestartedNew: restarted, StartTime: start,
		}
	}

	tests := []struct {
		name     string
		res      serviceResult
		target   ServiceTarget
		since    time.Time
		wantPass bool
	}{
		{
			name: "全部正常",
			res: serviceResult{
				ExpectCommit: "abc12345", AliveCount: 2,
				Instances: []instanceResult{
					mk(true, true, true, afterDeploy),
					mk(true, true, true, afterDeploy),
				},
			},
			target:   ServiceTarget{MinInstances: 2},
			since:    deployTime,
			wantPass: true,
		},
		{
			name: "存活实例不足",
			res: serviceResult{
				ExpectCommit: "abc12345", AliveCount: 1,
				Instances: []instanceResult{mk(true, true, true, afterDeploy)},
			},
			target:   ServiceTarget{MinInstances: 2},
			since:    deployTime,
			wantPass: false,
		},
		{
			name: "commit不匹配",
			res: serviceResult{
				ExpectCommit: "abc12345", AliveCount: 1,
				Instances: []instanceResult{mk(true, false, true, afterDeploy)},
			},
			target:   ServiceTarget{MinInstances: 1},
			since:    deployTime,
			wantPass: false,
		},
		{
			name: "进程未重启",
			res: serviceResult{
				ExpectCommit: "abc12345", AliveCount: 1,
				Instances: []instanceResult{mk(true, true, false, beforeDeploy)},
			},
			target:   ServiceTarget{MinInstances: 1},
			since:    deployTime,
			wantPass: false,
		},
		{
			name: "离线实例不参与commit判定但拉低存活数",
			res: serviceResult{
				ExpectCommit: "abc12345", AliveCount: 1,
				Instances: []instanceResult{
					mk(true, true, true, afterDeploy),
					mk(false, false, false, beforeDeploy), // 离线旧实例：不应导致失败
				},
			},
			target:   ServiceTarget{MinInstances: 1},
			since:    deployTime,
			wantPass: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, summary := judgeService(tt.res, tt.target, tt.since)
			if got != tt.wantPass {
				t.Errorf("judgeService() pass = %v, want %v (summary: %s)", got, tt.wantPass, summary)
			}
		})
	}
}

// TestResolveExpectCommit 覆盖预期 commit 的解析优先级。
func TestResolveExpectCommit(t *testing.T) {
	tests := []struct {
		configured, auto, want string
	}{
		{"auto", "abc123", "abc123"},
		{"", "abc123", "abc123"},
		{"AUTO", "abc123", "abc123"},
		{"deadbeef", "abc123", "deadbeef"}, // 写死值优先
	}
	for _, tt := range tests {
		if got := resolveExpectCommit(tt.configured, tt.auto); got != tt.want {
			t.Errorf("resolveExpectCommit(%q,%q) = %q, want %q", tt.configured, tt.auto, got, tt.want)
		}
	}
}

// TestParseSince 覆盖发版时刻解析（RFC3339 / unix / 空）。
func TestParseSince(t *testing.T) {
	if tm, err := parseSince(""); err != nil || !tm.IsZero() {
		t.Errorf("空字符串应返回零值无错误, got %v err=%v", tm, err)
	}
	if tm, err := parseSince("1751292000"); err != nil || tm.Unix() != 1751292000 {
		t.Errorf("unix 解析失败: %v err=%v", tm, err)
	}
	if _, err := parseSince("not-a-time"); err == nil {
		t.Error("非法时间应返回错误")
	}
}
