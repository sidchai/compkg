package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sidchai/compkg/pkg/health"
)

// instanceResult 单个实例的检测结论。
type instanceResult struct {
	Instance     string
	Alive        bool
	Commit       string
	BuildID      string
	StartTime    time.Time
	Pid          string
	Host         string
	CommitMatch  bool // 运行 commit == 预期 commit
	RestartedNew bool // startTime 晚于发版时刻
	reason       string
}

// serviceResult 单个服务的检测结论。
type serviceResult struct {
	Target       ServiceTarget
	ExpectCommit string
	Instances    []instanceResult
	AliveCount   int
	Pass         bool
	Summary      string
}

// checkService 检测单个服务：拉实例明细 → 逐实例三维比对 → 汇总判定。
//
// since 为发版开始时刻；startTime 晚于它才算"重启到新版本"。
// since 为零值表示不校验重启时间（仅校验 commit + 实例数）。
func checkService(ctx context.Context, reader *health.Reader, target ServiceTarget, expectCommit string, since time.Time) serviceResult {
	res := serviceResult{Target: target, ExpectCommit: expectCommit}

	instances, err := reader.Instances(ctx, target.Name)
	if err != nil {
		res.Pass = false
		res.Summary = fmt.Sprintf("读取实例失败: %v", err)
		return res
	}

	for _, inst := range instances {
		ir := instanceResult{
			Instance:  inst.Instance,
			Alive:     inst.Alive,
			Commit:    inst.Meta["commit"],
			BuildID:   inst.Meta["buildID"],
			Pid:       inst.Meta["pid"],
			Host:      inst.Meta["host"],
			StartTime: parseStartTime(inst.Meta["startTime"]),
		}
		// commit 比对：预期为空时跳过该维度（视为通过）。
		ir.CommitMatch = expectCommit == "" || ir.Commit == expectCommit
		// 重启时间比对：since 为零值时跳过。
		ir.RestartedNew = since.IsZero() || (!ir.StartTime.IsZero() && ir.StartTime.After(since))

		if inst.Alive {
			res.AliveCount++
		}
		ir.reason = buildReason(ir, expectCommit, since)
		res.Instances = append(res.Instances, ir)
	}

	res.Pass, res.Summary = judgeService(res, target, since)
	return res
}

// judgeService 综合判定服务是否发版成功。
func judgeService(res serviceResult, target ServiceTarget, since time.Time) (bool, string) {
	if res.AliveCount < target.MinInstances {
		return false, fmt.Sprintf("存活实例 %d < 期望 %d", res.AliveCount, target.MinInstances)
	}
	// 所有存活实例必须 commit 匹配 + 已重启到新版本。
	for _, ir := range res.Instances {
		if !ir.Alive {
			continue
		}
		if !ir.CommitMatch {
			return false, fmt.Sprintf("实例 %s 运行 commit=%s 与预期 %s 不一致", ir.Instance, emptyAs(ir.Commit, "?"), res.ExpectCommit)
		}
		if !ir.RestartedNew {
			return false, fmt.Sprintf("实例 %s 启动时间 %s 早于发版时刻，疑似未重启", ir.Instance, fmtTime(ir.StartTime))
		}
	}
	return true, fmt.Sprintf("全部 %d 个存活实例已更新到 %s", res.AliveCount, emptyAs(res.ExpectCommit, "(未校验commit)"))
}

// buildReason 生成单实例的简短结论说明。
func buildReason(ir instanceResult, expectCommit string, since time.Time) string {
	if !ir.Alive {
		return "离线"
	}
	var probs []string
	if !ir.CommitMatch {
		probs = append(probs, "commit不匹配")
	}
	if !ir.RestartedNew {
		probs = append(probs, "未重启")
	}
	if len(probs) == 0 {
		return "正常"
	}
	return strings.Join(probs, "+")
}

// BuildMarkdown 生成 Markdown 报告。
func BuildMarkdown(results []serviceResult, expectCommit string, since time.Time, allPass bool) string {
	var b strings.Builder
	b.WriteString("# 发版自动化服务检测报告\n\n")
	b.WriteString(fmt.Sprintf("- 检测时间：%s\n", time.Now().Format("2006-01-02 15:04:05")))
	b.WriteString(fmt.Sprintf("- 预期 commit：`%s`\n", emptyAs(expectCommit, "(未指定，跳过commit校验)")))
	if !since.IsZero() {
		b.WriteString(fmt.Sprintf("- 发版时刻：%s（启动时间须晚于此）\n", since.Format("2006-01-02 15:04:05")))
	} else {
		b.WriteString("- 发版时刻：未指定（跳过重启时间校验）\n")
	}
	overall := "✅ 全部通过"
	if !allPass {
		overall = "❌ 存在失败"
	}
	b.WriteString(fmt.Sprintf("- 总体结论：**%s**\n\n", overall))

	for _, r := range results {
		status := "✅ 成功"
		if !r.Pass {
			status = "❌ 失败"
		}
		risk := ""
		if r.Target.HighRisk {
			risk = "（高危）"
		}
		b.WriteString(fmt.Sprintf("## %s `%s`%s — %s\n\n", r.Target.Display, r.Target.Name, risk, status))
		b.WriteString(fmt.Sprintf("> %s\n\n", r.Summary))
		b.WriteString("| 实例 | 主机 | PID | 运行commit | BuildID | 启动时间 | 状态 | 结论 |\n")
		b.WriteString("|------|------|-----|-----------|---------|----------|------|------|\n")
		if len(r.Instances) == 0 {
			b.WriteString("| _无实例_ | - | - | - | - | - | - | - |\n")
		}
		for _, ir := range r.Instances {
			alive := "活"
			if !ir.Alive {
				alive = "离线"
			}
			b.WriteString(fmt.Sprintf("| %s | %s | %s | `%s` | `%s` | %s | %s | %s |\n",
				ir.Instance, emptyAs(ir.Host, "-"), emptyAs(ir.Pid, "-"),
				emptyAs(ir.Commit, "?"), emptyAs(ir.BuildID, "?"),
				fmtTime(ir.StartTime), alive, ir.reason))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func parseStartTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	sec, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02 15:04:05")
}

func emptyAs(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
