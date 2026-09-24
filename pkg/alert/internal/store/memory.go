// Package store 提供本机 StateStore。容量上限防止 10 万设备场景无界增长。
package store

import (
	"context"
	"sync"
	"time"

	"github.com/sidchai/compkg/pkg/alert/internal/core"
)

// MemoryStore 本机降级实现，带容量上限，禁止无界增长。
type MemoryStore struct {
	mu       sync.Mutex
	kv       map[string]memVal
	z        map[string]map[string]float64
	capacity int
}

type memVal struct {
	value  string
	expire time.Time
}

// NewMemoryStore 创建本机存储。capacity<=0 时默认 200000。
func NewMemoryStore(capacity int) *MemoryStore {
	if capacity <= 0 {
		capacity = 200000
	}
	return &MemoryStore{
		kv:       make(map[string]memVal),
		z:        make(map[string]map[string]float64),
		capacity: capacity,
	}
}

func (s *MemoryStore) evictLocked(now time.Time) {
	for k, v := range s.kv {
		if !v.expire.IsZero() && now.After(v.expire) {
			delete(s.kv, k)
		}
	}
}

func (s *MemoryStore) Get(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictLocked(time.Now())
	v, ok := s.kv[key]
	if !ok {
		return "", nil
	}
	return v.value, nil
}

func (s *MemoryStore) SetNX(_ context.Context, key, value string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.evictLocked(now)
	if v, ok := s.kv[key]; ok && (v.expire.IsZero() || now.Before(v.expire)) {
		return false, nil
	}
	if len(s.kv) >= s.capacity {
		return false, core.ErrCapacityExceeded
	}
	exp := time.Time{}
	if ttl > 0 {
		exp = now.Add(ttl)
	}
	s.kv[key] = memVal{value: value, expire: exp}
	return true, nil
}

func (s *MemoryStore) Set(_ context.Context, key, value string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.evictLocked(now)
	if _, ok := s.kv[key]; !ok && len(s.kv) >= s.capacity {
		return core.ErrCapacityExceeded
	}
	exp := time.Time{}
	if ttl > 0 {
		exp = now.Add(ttl)
	}
	s.kv[key] = memVal{value: value, expire: exp}
	return nil
}

func (s *MemoryStore) Del(_ context.Context, keys ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range keys {
		delete(s.kv, k)
	}
	return nil
}

func (s *MemoryStore) ZAdd(_ context.Context, key string, score float64, member string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	z := s.z[key]
	if z == nil {
		z = make(map[string]float64)
		s.z[key] = z
	}
	if _, ok := z[member]; !ok && len(z) >= s.capacity {
		return core.ErrCapacityExceeded
	}
	z[member] = score
	return nil
}

func (s *MemoryStore) ZRangeByScore(_ context.Context, key string, min, max float64, limit int) ([]core.ZMember, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	z := s.z[key]
	out := make([]core.ZMember, 0)
	for m, score := range z {
		if score >= min && score <= max {
			out = append(out, core.ZMember{Member: m, Score: score})
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (s *MemoryStore) ZRem(_ context.Context, key string, members ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	z := s.z[key]
	if z == nil {
		return nil
	}
	for _, m := range members {
		delete(z, m)
	}
	return nil
}

// Size 当前 kv 条数，供 TTL/容量单测。
func (s *MemoryStore) Size() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictLocked(time.Now())
	return len(s.kv)
}
