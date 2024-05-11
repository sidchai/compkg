package cache

import (
	"github.com/go-redis/redis/v8"
	"github.com/sidchai/compkg/pkg/trace"
)

type redisOptions struct {
	clientName string
	prefix     string
	opt        *redis.Options
	trace      *trace.Cache
}

var defaultRedisOptions = redisOptions{}

type RedisOption interface {
	apply(*redisOptions)
}

type funcRedisOption struct {
	f func(options *redisOptions)
}

func (fpo *funcRedisOption) apply(ro *redisOptions) {
	fpo.f(ro)
}

func newFuncProducerOption(f func(options *redisOptions)) *funcRedisOption {
	return &funcRedisOption{
		f: f,
	}
}

func WithClientName(clientName string) RedisOption {
	return newFuncProducerOption(func(o *redisOptions) {
		o.clientName = clientName
	})
}

func WithPrefix(prefix string) RedisOption {
	return newFuncProducerOption(func(o *redisOptions) {
		o.prefix = prefix
	})
}

func WithOptions(opt *redis.Options) RedisOption {
	return newFuncProducerOption(func(o *redisOptions) {
		o.opt = opt
	})
}

func WithTrace(trace *trace.Cache) RedisOption {
	return newFuncProducerOption(func(o *redisOptions) {
		o.trace = trace
	})
}
