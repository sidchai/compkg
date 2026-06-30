package scheduler

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultDiskSpillDirName  = "scheduler-spool"
	defaultDiskSpillMaxBytes = int64(100 * 1024 * 1024)
)

// ErrLocalBufferSpillFull 表示内存队列满且磁盘 spill 也因容量限制无法接纳新任务。
var ErrLocalBufferSpillFull = errors.New("scheduler: local buffer spill full")

// spoolTask 是落盘后的待重放任务记录。
//
// 文件名使用单调递增序号承载顺序，文件内容保留 SubmitOptions 和排障字段；
// 成功提交 scheduler 后删除该文件，进程重启时按序号升序扫描即可恢复 FIFO 语义。
type spoolTask struct {
	Seq        int64         `json:"seq"`
	Opts       SubmitOptions `json:"opts"`
	EnqueuedAt int64         `json:"enqueued_at"`
	Attempts   int           `json:"attempts"`
	SizeBytes  int64         `json:"size_bytes"`
}

// submitSpool 是 scheduler SDK 的磁盘级兜底队列。
//
// 设计约束：
//   - 仅服务 EnqueueTask；SubmitTask 同步语义保持不变
//   - 标准库文件队列，不引入 badger/bolt 依赖，降低 compkg 公共包升级风险
//   - 每条任务一个 JSON 文件，写入临时文件后原子 rename，避免进程崩溃留下半文件
//   - currentBytes 超过 maxBytes 前会先淘汰最旧任务；单条任务大于 maxBytes 直接拒绝
//   - replay 时先取最旧 N 条，提交成功后 delete，失败时保留文件等待下一轮
type submitSpool struct {
	dir      string
	maxBytes int64

	mu           sync.Mutex
	nextSeq      int64
	currentBytes int64
	files        []spoolFile
}

type spoolFile struct {
	seq  int64
	path string
	size int64
}

func newSubmitSpool(dir string, maxBytes int64) (*submitSpool, error) {
	if dir == "" {
		dir = filepath.Join(os.TempDir(), defaultDiskSpillDirName)
	}
	if maxBytes <= 0 {
		maxBytes = defaultDiskSpillMaxBytes
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create scheduler spool dir: %w", err)
	}
	s := &submitSpool{dir: dir, maxBytes: maxBytes, nextSeq: time.Now().UnixNano()}
	if err := s.reloadLocked(); err != nil {
		return nil, err
	}
	return s, nil
}

// push 将任务持久化到磁盘队列。
//
// 容量策略：若加入新任务会超过 maxBytes，先按 FIFO 删除最旧文件腾空间；
// 如果单条任务自身已经超过 maxBytes，则直接返回 ErrLocalBufferSpillFull。
func (s *submitSpool) push(t bufferedTask) error {
	if s == nil {
		return ErrLocalBufferSpillFull
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	seq := s.nextSeq
	s.nextSeq++
	st := spoolTask{
		Seq:        seq,
		Opts:       t.opts,
		EnqueuedAt: t.enqueuedAt.UnixNano(),
		Attempts:   t.attempts,
	}
	data, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("marshal scheduler spool task: %w", err)
	}
	st.SizeBytes = int64(len(data))
	data, err = json.Marshal(st)
	if err != nil {
		return fmt.Errorf("marshal scheduler spool task with size: %w", err)
	}
	size := int64(len(data))
	if size > s.maxBytes {
		return ErrLocalBufferSpillFull
	}
	for s.currentBytes+size > s.maxBytes && len(s.files) > 0 {
		oldest := s.files[0]
		if err := os.Remove(oldest.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("evict scheduler spool task %s: %w", oldest.path, err)
		}
		s.currentBytes -= oldest.size
		s.files = s.files[1:]
	}
	if s.currentBytes+size > s.maxBytes {
		return ErrLocalBufferSpillFull
	}

	finalPath := filepath.Join(s.dir, fmt.Sprintf("%020d.json", seq))
	tmpPath := finalPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("write scheduler spool tmp: %w", err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename scheduler spool task: %w", err)
	}
	s.files = append(s.files, spoolFile{seq: seq, path: finalPath, size: size})
	s.currentBytes += size
	return nil
}

// drain 读取最多 max 条磁盘任务但不删除文件；调用方提交成功后必须 ack。
func (s *submitSpool) drain(max int) ([]spoolTask, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.files) == 0 {
		return nil, nil
	}
	if max <= 0 || max > len(s.files) {
		max = len(s.files)
	}
	out := make([]spoolTask, 0, max)
	for _, f := range s.files[:max] {
		data, err := os.ReadFile(f.path)
		if err != nil {
			return nil, fmt.Errorf("read scheduler spool task %s: %w", f.path, err)
		}
		var st spoolTask
		if err := json.Unmarshal(data, &st); err != nil {
			return nil, fmt.Errorf("decode scheduler spool task %s: %w", f.path, err)
		}
		out = append(out, st)
	}
	return out, nil
}

// ack 删除已成功提交的磁盘任务。
func (s *submitSpool) ack(seq int64) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, f := range s.files {
		if f.seq != seq {
			continue
		}
		if err := os.Remove(f.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove scheduler spool task %s: %w", f.path, err)
		}
		s.currentBytes -= f.size
		s.files = append(s.files[:i], s.files[i+1:]...)
		return nil
	}
	return nil
}

func (s *submitSpool) len() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.files)
}

func (s *submitSpool) bytes() int64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.currentBytes
}

func (s *submitSpool) reloadLocked() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return fmt.Errorf("read scheduler spool dir: %w", err)
	}
	files := make([]spoolFile, 0, len(entries))
	var total int64
	var maxSeq int64
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		seqText := strings.TrimSuffix(e.Name(), ".json")
		seq, err := strconv.ParseInt(seqText, 10, 64)
		if err != nil {
			continue
		}
		path := filepath.Join(s.dir, e.Name())
		info, err := e.Info()
		if err != nil {
			return fmt.Errorf("stat scheduler spool file %s: %w", path, err)
		}
		files = append(files, spoolFile{seq: seq, path: path, size: info.Size()})
		total += info.Size()
		if seq > maxSeq {
			maxSeq = seq
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].seq < files[j].seq })
	s.files = files
	s.currentBytes = total
	if maxSeq >= s.nextSeq {
		s.nextSeq = maxSeq + 1
	}
	for s.currentBytes > s.maxBytes && len(s.files) > 0 {
		oldest := s.files[0]
		if err := os.Remove(oldest.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("evict oversized scheduler spool %s: %w", oldest.path, err)
		}
		s.currentBytes -= oldest.size
		s.files = s.files[1:]
	}
	return nil
}
