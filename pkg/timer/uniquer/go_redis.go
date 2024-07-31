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

func NewUniqueGoRedis(ctx context.Context, redis *redis.Client) *uniqueGoRedis {
	return &uniqueGoRedis{redis, ctx}
}

func (u *uniqueGoRedis) SetLimit(key, value string) error {

	err := u.Redis.SetNX(u.Ctx, key, value, expireSecond).Err()
	if err != nil {
		fmt.Println("redis setNx fail, err: ", err)
		return err
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
