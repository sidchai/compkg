package redis_lock

import (
	"context"
	"errors"
	"github.com/go-redis/redis/v8"
	"math/rand"
	"strconv"
	"time"
)

const (
	randomLen       = 16
	tolerance       = 500 // milliseconds
	millisPerSecond = 1000
	lockCommand     = `if redis.call("GET", KEYS[1]) == ARGV[1] then
    redis.call("SET", KEYS[1], ARGV[1], "PX", ARGV[2])
    return "OK"
else
    return redis.call("SET", KEYS[1], ARGV[1], "NX", "PX", ARGV[2])
end`
	delCommand = `if redis.call("GET", KEYS[1]) == ARGV[1] then
    return redis.call("DEL", KEYS[1])
else
    return 0
end`
)

type RedisLock struct {
	store *redis.Client
	key   string
	id    string
	opts  *Options
	ctx   context.Context
}

type Options struct {
	LockSeconds uint32
	WaitTimeout time.Duration
	WaitRetry   time.Duration
}

func init() {
	rand.Seed(time.Now().UnixNano())
}

// NewRedisLock returns a RedisLock.
func NewRedisLock(store *redis.Client, key string, opt *Options, ctx context.Context) *RedisLock {
	if opt == nil {
		opt = &Options{LockSeconds: 3, WaitRetry: 100 * time.Millisecond}
	}
	if opt.WaitRetry == 0 {
		opt.WaitRetry = 100 * time.Millisecond
	}
	if opt.LockSeconds == 0 {
		opt.LockSeconds = 10
	}
	return &RedisLock{
		store: store,
		key:   key,
		id:    Randn(randomLen),
		opts:  opt,
		ctx:   ctx,
	}
}

// TryLock 尝试获取锁
func (rl *RedisLock) TryLock() error {
	var stop time.Time
	if rl.opts.WaitTimeout > 0 {
		stop = time.Now().Add(rl.opts.WaitTimeout)
	}
	for {
		lock, err := rl.Lock()
		if err == nil && lock {
			return nil
		}
		if rl.opts.WaitTimeout == 0 {
			break
		}
		if time.Now().Add(rl.opts.WaitRetry).After(stop) {
			break
		}
		time.Sleep(rl.opts.WaitRetry)
	}
	return errors.New("get lock failed")
}

// Lock acquires the lock.
func (rl *RedisLock) Lock() (bool, error) {
	resp, err := rl.store.Eval(rl.ctx, lockCommand, []string{rl.key}, rl.id, strconv.Itoa(int(rl.opts.LockSeconds)*millisPerSecond+tolerance)).Result()
	if err != nil {
		return false, err
	} else if resp == nil {
		return false, nil
	}
	reply, ok := resp.(string)
	if ok && reply == "OK" {
		return true, nil
	}
	return false, nil
}

// UnLock releases the lock.
func (rl *RedisLock) UnLock() (bool, error) {
	resp, err := rl.store.Eval(rl.ctx, delCommand, []string{rl.key}, rl.id).Result()
	if err != nil {
		return false, err
	}
	reply, ok := resp.(int64)
	if !ok {
		return false, nil
	}
	return reply == 1, nil
}
