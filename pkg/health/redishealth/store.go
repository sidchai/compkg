// Package redishealth 提供 health.Store/MetaStore 的 go-redis/v8 实现，
// 以及一行式启动心跳上报的便捷函数，供所有业务服务零样板复用。
//
// 设计：health 核心包保持纯抽象（不依赖任何 redis 实现），redis 耦合隔离在本子包。
// 业务服务只需 redishealth.Start(ctx, client, "服务名") 即可接入版本指纹心跳。
package redishealth

import (
	"context"

	redis "github.com/go-redis/redis/v8"

	"github.com/sidchai/compkg/pkg/health"
)

// Store 用 go-redis/v8 客户端实现 health.Store + health.MetaStore。
type Store struct {
	client redis.UniversalClient
}

// NewStore 基于 go-redis 通用客户端构造 Store（兼容单实例 *redis.Client 与集群）。
func NewStore(client redis.UniversalClient) *Store {
	return &Store{client: client}
}

func (s *Store) ZAdd(ctx context.Context, key string, score float64, member interface{}) error {
	return s.client.ZAdd(ctx, key, &redis.Z{Score: score, Member: member}).Err()
}

func (s *Store) ZRemRangeByScore(ctx context.Context, key, min, max string) error {
	return s.client.ZRemRangeByScore(ctx, key, min, max).Err()
}

func (s *Store) ZRem(ctx context.Context, key string, member interface{}) error {
	return s.client.ZRem(ctx, key, member).Err()
}

func (s *Store) ZCount(ctx context.Context, key, min, max string) (int64, error) {
	return s.client.ZCount(ctx, key, min, max).Result()
}

func (s *Store) ZCard(ctx context.Context, key string) (int64, error) {
	return s.client.ZCard(ctx, key).Result()
}

func (s *Store) HSet(ctx context.Context, key, field, value string) error {
	return s.client.HSet(ctx, key, field, value).Err()
}

func (s *Store) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	return s.client.HGetAll(ctx, key).Result()
}

func (s *Store) HDel(ctx context.Context, key string, fields ...string) error {
	return s.client.HDel(ctx, key, fields...).Err()
}

func (s *Store) ZRangeByScoreWithScores(ctx context.Context, key, min, max string) ([]health.ScoredMember, error) {
	zs, err := s.client.ZRangeByScoreWithScores(ctx, key, &redis.ZRangeBy{Min: min, Max: max}).Result()
	if err != nil {
		return nil, err
	}
	out := make([]health.ScoredMember, 0, len(zs))
	for _, z := range zs {
		member, _ := z.Member.(string)
		out = append(out, health.ScoredMember{Member: member, Score: z.Score})
	}
	return out, nil
}
