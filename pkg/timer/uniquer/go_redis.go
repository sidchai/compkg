package uniquer

import (
	"context"
	"errors"
	"fmt"
	"github.com/go-redis/redis/v8"
	"time"
)

type uniqueGoRedis struct {
	Redis *redis.Client
	Ctx   context.Context
}

var expireSecond = 15 * time.Second

// ErrUniqueLimitHeld SetNX 未抢到锁。timer.SetLimit 失败时 timerAction 会跳过本轮。
var ErrUniqueLimitHeld = errors.New("unique limit held")

func NewUniqueGoRedis(ctx context.Context, redis *redis.Client) *uniqueGoRedis {
	return &uniqueGoRedis{redis, ctx}
}

func (u *uniqueGoRedis) SetLimit(key, value string) error {
	// 必须看 bool：key 已存在时 SetNX 返回 false, nil，只判断 error 会导致多实例同时执行。
	ok, err := u.Redis.SetNX(u.Ctx, key, value, expireSecond).Result()
	if err != nil {
		fmt.Println("redis setNx fail, err: ", err)
		return err
	}
	if !ok {
		return ErrUniqueLimitHeld
	}
	return nil
}

func (u *uniqueGoRedis) DeleteLimit(key, value string) error {
	txf := func(tx *redis.Tx) error {
		val := tx.Get(u.Ctx, key).Val()
		if val != value {
			return errors.New("值不一致")
		}
		_, err := tx.TxPipelined(u.Ctx, func(pipe redis.Pipeliner) error {
			if err := pipe.Del(u.Ctx, key).Err(); err != nil {
				return err
			}
			return nil
		})
		return err
	}
	err := u.Redis.Watch(u.Ctx, txf, key)
	if err != nil {
		return err
	}

	return nil
}

func (u *uniqueGoRedis) RefreshLimit(key, value string) error {
	txf := func(tx *redis.Tx) error {
		val := tx.Get(u.Ctx, key).Val()
		if val != value {
			return errors.New("值不一致")
		}
		_, err := tx.TxPipelined(u.Ctx, func(pipe redis.Pipeliner) error {
			if err := pipe.Expire(u.Ctx, key, expireSecond).Err(); err != nil {
				return err
			}
			return nil
		})
		return err
	}
	err := u.Redis.Watch(u.Ctx, txf, key)
	if err != nil {
		return err
	}

	return nil
}
