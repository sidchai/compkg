package cache

import (
	"context"
	"errors"
	"fmt"
	"github.com/go-redis/redis/v8"
	"github.com/sidchai/compkg/pkg/timex"
	"github.com/sidchai/compkg/pkg/trace"
	"go.uber.org/zap"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	redisClients = sync.Map{}
	ctx          = context.Background()
)

type Redis struct {
	ctx           context.Context
	client        *redis.Client
	clusterClient *redis.ClusterClient
	trace         *trace.Cache
	prefix        string
}

const (
	DefaultRedisClient = "default-redis-client"
	MinIdleConns       = 50
	PoolSize           = 20
	MaxRetries         = 3
)

func setDefaultOptions(opt *redis.Options) {
	if opt.DialTimeout == 0 {
		opt.DialTimeout = 2 * time.Second
	}

	if opt.ReadTimeout == 0 {
		//默认值为3秒
		opt.ReadTimeout = 3 * time.Second
	}

	if opt.WriteTimeout == 0 {
		//默认值与ReadTimeout相等
		opt.WriteTimeout = 3 * time.Second
	}

	if opt.PoolTimeout == 0 {
		//默认为ReadTimeout + 1秒（4s）
		opt.PoolTimeout = 4 * time.Second
	}
	if opt.IdleTimeout == 0 {
		//默认值为5秒
		opt.IdleTimeout = 5 * time.Second
	}
}

func setDefaultClusterOptions(opt *redis.ClusterOptions) {
	if opt.DialTimeout == 0 {
		opt.DialTimeout = 2 * time.Second
	}

	if opt.ReadTimeout == 0 {
		//默认值为3秒
		opt.ReadTimeout = 2 * time.Second
	}

	if opt.ReadTimeout == 0 {
		//默认值与ReadTimeout相等
		opt.ReadTimeout = 2 * time.Second
	}

	if opt.PoolTimeout == 0 {
		//默认为ReadTimeout + 1秒（4s）
		opt.PoolTimeout = 10 * time.Second
	}
	if opt.IdleTimeout == 0 {
		//默认值为5秒
		opt.IdleTimeout = 10 * time.Second
	}
}

func NewRedis(opts ...RedisOption) error {
	copyOpt := defaultRedisOptions
	po := &copyOpt
	for _, opt := range opts {
		opt.apply(po)
	}
	if len(po.clientName) == 0 {
		return errors.New("empty client name")
	}

	if len(po.opt.Addr) == 0 {
		return errors.New("empty addr")
	}

	setDefaultOptions(po.opt)
	client := redis.NewClient(po.opt)

	ctx1, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx1).Err(); err != nil {
		return err
	}
	redisClient := &Redis{
		ctx:    ctx,
		client: client,
		trace:  po.trace,
		prefix: po.prefix,
	}
	redisClients.Store(po.clientName, redisClient)
	return nil
}

//func InitClusterRedis(clientName string, opt *redis.ClusterOptions, trace *trace.Cache) error {
//	if len(clientName) == 0 {
//		return errors.New("empty client name")
//	}
//	if len(opt.Addrs) == 0 {
//		return errors.New("empty addrs")
//	}
//	setDefaultClusterOptions(opt)
//	//NewClusterClient执行过程中会连接redis集群并, 并尝试发送("cluster", "info")指令去进行多次连接,
//	//如果这里传入很多连接地址，并且连接地址都不可用的情况下会阻塞很长时间
//	client := redis.NewClusterClient(opt)
//
//	if err := client.Ping(context.Background()).Err(); err != nil {
//		return err
//	}
//	redisClients[clientName] = &Redis{
//		clusterClient: client,
//	}
//	return nil
//}

func GetRedisClient(name string) *Redis {
	if client, ok := redisClients.Load(name); ok {
		return client.(*Redis)
	}
	return nil
}

func GetRedisClusterClient(name string) *Redis {
	if client, ok := redisClients.Load(name); ok {
		return client.(*Redis)
	}
	return nil
}

// Set set some <key,value> into redis
func (r *Redis) Set(key string, value interface{}, ttl time.Duration) error {
	if len(key) == 0 {
		return errors.New("empty key")
	}
	key = fmt.Sprintf("%s:%s", r.prefix, key)
	ts := time.Now()
	defer func() {
		if r.trace == nil || r.trace.Logger == nil {
			return
		}
		costMillisecond := time.Since(ts).Milliseconds()

		if !r.trace.AlwaysTrace && costMillisecond < r.trace.SlowLoggerMillisecond {
			return
		}
		r.trace.TraceTime = timex.GetTimeByTimestamp(time.Now().Unix())
		r.trace.CMD = "set"
		r.trace.Key = key
		r.trace.Value = value
		r.trace.TTL = ttl.Minutes()
		r.trace.CostMillisecond = costMillisecond
		r.trace.Logger.Warn("redis-trace", zap.Any("", r.trace))
	}()

	if r.client != nil {
		if err := r.client.Set(r.ctx, key, value, ttl).Err(); err != nil {
			return err
		}
		return nil
	}

	//集群版
	if err := r.clusterClient.Set(r.ctx, key, value, ttl).Err(); err != nil {
		return err
	}
	return nil
}

// Get get some key from redis
func (r *Redis) Get(key string) interface{} {
	if len(key) == 0 {
		fmt.Println("empty key")
		return nil
	}
	key = fmt.Sprintf("%s:%s", r.prefix, key)
	ts := time.Now()
	defer func() {
		if r.trace == nil || r.trace.Logger == nil {
			return
		}
		costMillisecond := time.Since(ts).Milliseconds()

		if !r.trace.AlwaysTrace && costMillisecond < r.trace.SlowLoggerMillisecond {
			return
		}
		r.trace.TraceTime = timex.GetTimeByTimestamp(time.Now().Unix())
		r.trace.CMD = "get"
		r.trace.Key = key
		r.trace.Value = ""
		r.trace.CostMillisecond = costMillisecond
		r.trace.Logger.Warn("redis-trace", zap.Any("", r.trace))
	}()

	if r.client != nil {
		value, err := r.client.Get(r.ctx, key).Result()
		if err != nil && err != redis.Nil {
			fmt.Printf("redis get key: %s err %v\n", key, err)
		}
		return value
	}

	value, err := r.clusterClient.Get(r.ctx, key).Result()
	if err != nil && err != redis.Nil {
		fmt.Printf("redis get key: %s err %v\n", key, err)
	}
	return value
}

func (r *Redis) GetStr(key string) (value string, err error) {
	if len(key) == 0 {
		err = errors.New("empty key")
		return
	}
	key = fmt.Sprintf("%s:%s", r.prefix, key)
	ts := time.Now()
	defer func() {
		if r.trace == nil || r.trace.Logger == nil {
			return
		}
		costMillisecond := time.Since(ts).Milliseconds()

		if !r.trace.AlwaysTrace && costMillisecond < r.trace.SlowLoggerMillisecond {
			return
		}
		r.trace.TraceTime = timex.GetTimeByTimestamp(time.Now().Unix())
		r.trace.CMD = "get"
		r.trace.Key = key
		r.trace.Value = value
		r.trace.CostMillisecond = costMillisecond
		r.trace.Logger.Warn("redis-trace", zap.Any("", r.trace))
	}()

	if r.client != nil {
		value, err = r.client.Get(r.ctx, key).Result()
		if err != nil && err != redis.Nil {
			return "", err
		}
		return
	}

	value, err = r.clusterClient.Get(r.ctx, key).Result()
	if err != nil && err != redis.Nil {
		return "", err
	}
	return
}

// TTL get some key from redis
func (r *Redis) TTL(key string) (time.Duration, error) {
	if len(key) == 0 {
		return 0, errors.New("empty key")
	}
	key = fmt.Sprintf("%s:%s", r.prefix, key)
	if r.client != nil {
		ttl, err := r.client.TTL(r.ctx, key).Result()
		if err != nil && err != redis.Nil {
			return -1, err
		}
		return ttl, nil
	}
	ttl, err := r.clusterClient.TTL(r.ctx, key).Result()
	if err != nil && err != redis.Nil {
		return -1, err
	}

	return ttl, nil
}

// Expire expire some key
func (r *Redis) Expire(key string, ttl time.Duration) (bool, error) {
	if len(key) == 0 {
		return false, errors.New("empty key")
	}
	key = fmt.Sprintf("%s:%s", r.prefix, key)
	if r.client != nil {
		ok, err := r.client.Expire(r.ctx, key, ttl).Result()
		return ok, err
	}
	ok, err := r.clusterClient.Expire(r.ctx, key, ttl).Result()
	return ok, err
}

// ExpireAt expire some key at some time
func (r *Redis) ExpireAt(key string, ttl time.Time) (bool, error) {
	if len(key) == 0 {
		return false, errors.New("empty key")
	}
	key = fmt.Sprintf("%s:%s", r.prefix, key)
	if r.client != nil {
		ok, err := r.client.ExpireAt(r.ctx, key, ttl).Result()
		return ok, err
	}
	ok, err := r.clusterClient.ExpireAt(r.ctx, key, ttl).Result()
	return ok, err

}

func (r *Redis) Delete(key string) error {
	if len(key) == 0 {
		return errors.New("empty key")
	}
	key = fmt.Sprintf("%s:%s", r.prefix, key)
	ts := time.Now()
	var value int64
	var err error
	defer func() {
		if r.trace == nil || r.trace.Logger == nil {
			return
		}
		costMillisecond := time.Since(ts).Milliseconds()

		if !r.trace.AlwaysTrace && costMillisecond < r.trace.SlowLoggerMillisecond {
			return
		}
		r.trace.TraceTime = timex.GetTimeByTimestamp(time.Now().Unix())
		r.trace.CMD = "del"
		r.trace.Key = key
		r.trace.Value = strconv.FormatInt(value, 10)
		r.trace.CostMillisecond = costMillisecond
		r.trace.Logger.Warn("redis-trace", zap.Any("", r.trace))
	}()

	if r.client != nil {
		_, err = r.client.Del(r.ctx, key).Result()
		return err
	}

	//集群版
	_, err = r.clusterClient.Del(r.ctx, key).Result()
	return err
}

func (r *Redis) Incr(key string) (value int64, err error) {
	if len(key) == 0 {
		return 0, errors.New("empty key")
	}
	key = fmt.Sprintf("%s:%s", r.prefix, key)
	ts := time.Now()
	defer func() {
		if r.trace == nil || r.trace.Logger == nil {
			return
		}
		costMillisecond := time.Since(ts).Milliseconds()

		if !r.trace.AlwaysTrace && costMillisecond < r.trace.SlowLoggerMillisecond {
			return
		}
		r.trace.TraceTime = timex.GetTimeByTimestamp(time.Now().Unix())
		r.trace.CMD = "Incr"
		r.trace.Key = key
		r.trace.Value = strconv.FormatInt(value, 10)
		r.trace.CostMillisecond = costMillisecond
		r.trace.Logger.Warn("redis-trace", zap.Any("", r.trace))
	}()
	if r.client != nil {
		value, err = r.client.Incr(r.ctx, key).Result()
		return
	}
	value, err = r.clusterClient.Incr(r.ctx, key).Result()
	return
}

func (r *Redis) HGet(key, field string) interface{} {
	if len(key) == 0 {
		fmt.Println("empty key")
		return nil
	}
	key = fmt.Sprintf("%s:%s", r.prefix, key)
	ts := time.Now()
	defer func() {
		if r.trace == nil || r.trace.Logger == nil {
			return
		}
		costMillisecond := time.Since(ts).Milliseconds()

		if !r.trace.AlwaysTrace && costMillisecond < r.trace.SlowLoggerMillisecond {
			return
		}
		r.trace.TraceTime = timex.GetTimeByTimestamp(time.Now().Unix())
		r.trace.CMD = "HGet"
		r.trace.Key = key
		r.trace.Value = ""
		r.trace.CostMillisecond = costMillisecond
		r.trace.Logger.Warn("redis-trace", zap.Any("", r.trace))
	}()

	if r.client != nil {
		value, err := r.client.HGet(r.ctx, key, field).Result()
		if err != nil && err != redis.Nil {
			fmt.Printf("redis get key: %s err %v\n", key, err)
		}
		return value
	}

	value, err := r.clusterClient.HGet(r.ctx, key, field).Result()
	if err != nil && err != redis.Nil {
		fmt.Printf("redis get key: %s err %v\n", key, err)
	}
	return value
}

func (r *Redis) HSet(key string, values ...interface{}) (value int64, err error) {
	if len(key) == 0 {
		return 0, errors.New("empty key")
	}
	key = fmt.Sprintf("%s:%s", r.prefix, key)
	ts := time.Now()
	defer func() {
		if r.trace == nil || r.trace.Logger == nil {
			return
		}
		costMillisecond := time.Since(ts).Milliseconds()

		if !r.trace.AlwaysTrace && costMillisecond < r.trace.SlowLoggerMillisecond {
			return
		}
		r.trace.TraceTime = timex.GetTimeByTimestamp(time.Now().Unix())
		r.trace.CMD = "HSet"
		r.trace.Key = key
		r.trace.Value = strconv.FormatInt(value, 10)
		r.trace.CostMillisecond = costMillisecond
		r.trace.Logger.Warn("redis-trace", zap.Any("", r.trace))
	}()
	if r.client != nil {
		value, err = r.client.HSet(r.ctx, key, values).Result()
		return
	}
	value, err = r.clusterClient.HSet(r.ctx, key, values).Result()
	return
}

func (r *Redis) RPush(key string, values ...interface{}) (value int64, err error) {
	if len(key) == 0 {
		return 0, errors.New("empty key")
	}
	key = fmt.Sprintf("%s:%s", r.prefix, key)
	ts := time.Now()
	defer func() {
		if r.trace == nil || r.trace.Logger == nil {
			return
		}
		costMillisecond := time.Since(ts).Milliseconds()

		if !r.trace.AlwaysTrace && costMillisecond < r.trace.SlowLoggerMillisecond {
			return
		}
		r.trace.TraceTime = timex.GetTimeByTimestamp(time.Now().Unix())
		r.trace.CMD = "RPush"
		r.trace.Key = key
		r.trace.Value = strconv.FormatInt(value, 10)
		r.trace.CostMillisecond = costMillisecond
		r.trace.Logger.Warn("redis-trace", zap.Any("", r.trace))
	}()
	if r.client != nil {
		value, err = r.client.RPush(r.ctx, key, values).Result()
		return
	}
	value, err = r.clusterClient.RPush(r.ctx, key, values).Result()
	return
}

// Close close redis client
func (r *Redis) Close() error {
	return r.client.Close()
}

// Version redis server version
func (r *Redis) Version() string {
	if r.client != nil {
		server := r.client.Info(r.ctx, "server").Val()
		spl1 := strings.Split(server, "# Server")
		spl2 := strings.Split(spl1[1], "redis_version:")
		spl3 := strings.Split(spl2[1], "redis_git_sha1:")
		return spl3[0]
	}
	server := r.clusterClient.Info(r.ctx, "server").Val()
	spl1 := strings.Split(server, "# Server")
	spl2 := strings.Split(spl1[1], "redis_version:")
	spl3 := strings.Split(spl2[1], "redis_git_sha1:")
	return spl3[0]

}
