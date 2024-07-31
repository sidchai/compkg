package cache

import (
	"github.com/go-redis/redis/v8"
	_ "github.com/sidchai/compkg/pkg/compression/impl"
	"github.com/sidchai/compkg/pkg/serialization"
	_ "github.com/sidchai/compkg/pkg/serialization/impl"
	"github.com/sidchai/compkg/pkg/trace"
	"testing"
	"time"
)

type UserTest struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func TestGet(t *testing.T) {
	key := "test"

	user := UserTest{
		ID:   1,
		Name: "imooc",
	}
	getSerialization := serialization.GetSerialization("sonic")
	if getSerialization == nil {
		t.Errorf("GetSerialization err %v", getSerialization)
	}
	userByte, err := getSerialization.MarshalAndCompression(user)
	if err != nil {
		t.Errorf("MarshalAndCompression err %v", err)
	}
	opts := &redis.Options{
		Addr:     "127.0.0.1:6379",
		Password: "crs-pjns4g0f:Saisiyun0427",
		DB:       0,
	}
	NewRedis(WithClientName(DefaultRedisClient), WithOptions(opts), WithTrace(new(trace.Cache)), WithPrefix("sidchai"))
	redisClient := GetRedisClient(DefaultRedisClient)
	err = redisClient.Set(key, string(userByte), time.Minute)
	if err != nil {
		t.Error("UnmarshalAndCompression error", err)
	}
	val, _ := redisClient.GetStr(key)
	output := UserTest{}
	err = getSerialization.UnmarshalAndCompression([]byte(val), &output)
	if err != nil {
		t.Error("UnmarshalAndCompression error", val, err)
	}
	t.Log(output)
}
