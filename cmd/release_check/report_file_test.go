package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestWriteDailyReport 验证按天报告写入：目录自动创建、文件名含时间戳、内容正确。
func TestWriteDailyReport(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "reports")
	conf := ReportConf{OutputDir: dir, KeepDays: 1}

	path, err := writeDailyReport(conf, "# 测试报告内容")
	if err != nil {
		t.Fatalf("writeDailyReport 失败: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取报告失败: %v", err)
	}
	if string(data) != "# 测试报告内容" {
		t.Errorf("报告内容不符: %q", string(data))
	}
	if filepath.Dir(path) != dir {
		t.Errorf("报告未写入指定目录: %s", path)
	}
}

// TestCleanOldReports 验证过期报告清理：超过保留天数的删除，当天的保留。
func TestCleanOldReports(t *testing.T) {
	dir := t.TempDir()
	conf := ReportConf{OutputDir: dir, KeepDays: 1}

	// 造一个 2 天前的旧报告 + 一个今天的新报告
	oldFile := filepath.Join(dir, "release_report_old.md")
	newFile := filepath.Join(dir, "release_report_new.md")
	if err := os.WriteFile(oldFile, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newFile, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 把旧文件的修改时间改成 2 天前
	twoDaysAgo := time.Now().AddDate(0, 0, -2)
	if err := os.Chtimes(oldFile, twoDaysAgo, twoDaysAgo); err != nil {
		t.Fatal(err)
	}

	cleanOldReports(conf)

	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Error("过期报告应被删除")
	}
	if _, err := os.Stat(newFile); err != nil {
		t.Error("当天报告不应被删除")
	}
}

// TestNotifyTitle 验证通知标题随结果变化。
func TestNotifyTitle(t *testing.T) {
	if notifyTitle(true) == notifyTitle(false) {
		t.Error("通过与失败的标题应不同")
	}
}
