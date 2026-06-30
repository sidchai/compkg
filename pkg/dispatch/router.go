package dispatch

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/go-redis/redis/v8"
)

const (
	NodeAliveKeyFmt           = "iot:nodes:alive:%s"
	NodeInstructionChannelFmt = "iot:instruction:%s"

	// InstructionPayloadSchema 是节点私有指令通道 envelope payload 的版本标识。
	// payload 统一为 JSON envelope，可携带 W3C TraceContext carrier 续接分布式 trace。
	InstructionPayloadSchema = "queue.v1"
)

type routeValueArray struct {
	Schema    string   `json:"_s"`
	Addresses []string `json:"addrs"`
}

type routeValueSingle struct {
	Schema     string `json:"_s"`
	InstanceId string `json:"addr"`
}

// InstructionPayload 是节点私有指令通道（NodeInstructionChannelFmt）的 envelope 格式。
// QueueId 为待消费的热表行 ID；Trace 为可选的 W3C TraceContext carrier（含 traceparent/tracestate）。
type InstructionPayload struct {
	Schema  string            `json:"_s"`
	QueueId int64             `json:"queueId"`
	Trace   map[string]string `json:"_trace,omitempty"`
}

func LookupDeviceNode(ctx context.Context, redisClient redis.Cmdable, deviceRouteKey string, deviceNo string) (string, error) {
	if redisClient == nil || strings.TrimSpace(deviceRouteKey) == "" || strings.TrimSpace(deviceNo) == "" {
		return "", nil
	}
	raw, err := redisClient.HGet(ctx, deviceRouteKey, deviceNo).Result()
	if err == redis.Nil || strings.TrimSpace(raw) == "" {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return lookupNodeFromRaw(ctx, redisClient, strings.TrimSpace(raw))
}

// PublishToNode 向指定通讯节点的私有通道发布 envelope payload（不携带 trace）。
// 用于内部任务流转或不需要 trace 的场景；常规业务请优先用 PublishToNodeWithTrace。
func PublishToNode(ctx context.Context, redisClient redis.Cmdable, instanceId string, queueId int64) error {
	return PublishToNodeWithTrace(ctx, redisClient, instanceId, queueId, nil)
}

// PublishToNodeWithTrace 向指定通讯节点的私有通道发布 envelope payload。
// traceCarrier 可为 nil；非 nil 时一般由 trace.Inject 写入（含 traceparent/tracestate），
// 用于通讯节点订阅端续接分布式 trace 链路。
func PublishToNodeWithTrace(ctx context.Context, redisClient redis.Cmdable, instanceId string, queueId int64, traceCarrier map[string]string) error {
	if redisClient == nil || strings.TrimSpace(instanceId) == "" || queueId <= 0 {
		return nil
	}
	payload := InstructionPayload{
		Schema:  InstructionPayloadSchema,
		QueueId: queueId,
		Trace:   traceCarrier,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return redisClient.Publish(ctx, fmt.Sprintf(NodeInstructionChannelFmt, instanceId), raw).Err()
}

// ParseInstructionPayload 解析节点私有指令通道收到的 envelope payload。
// 订阅端拿到 traceCarrier 后可用 trace.Extract 续接 ctx；为 nil 表示发布端未注入 trace。
func ParseInstructionPayload(raw string) (queueId int64, traceCarrier map[string]string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil, fmt.Errorf("empty instruction payload")
	}
	if raw[0] != '{' {
		return 0, nil, fmt.Errorf("unsupported instruction payload format (expect json envelope): %s", raw)
	}
	var payload InstructionPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return 0, nil, fmt.Errorf("decode instruction envelope: %w", err)
	}
	if payload.QueueId <= 0 {
		return 0, nil, fmt.Errorf("invalid queueId in envelope: %d", payload.QueueId)
	}
	return payload.QueueId, payload.Trace, nil
}

func IsNodeAlive(ctx context.Context, redisClient redis.Cmdable, instanceId string) bool {
	if redisClient == nil || strings.TrimSpace(instanceId) == "" {
		return false
	}
	n, err := redisClient.Exists(ctx, fmt.Sprintf(NodeAliveKeyFmt, instanceId)).Result()
	return err == nil && n > 0
}

func lookupNodeFromRaw(ctx context.Context, redisClient redis.Cmdable, raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	switch raw[0] {
	case '[':
		var addresses []string
		if err := json.Unmarshal([]byte(raw), &addresses); err != nil {
			return "", err
		}
		return firstAliveNode(ctx, redisClient, addresses), nil
	case '{':
		var probe struct {
			Schema string `json:"_s"`
		}
		if err := json.Unmarshal([]byte(raw), &probe); err != nil {
			return "", err
		}
		switch probe.Schema {
		case "addrs.v1":
			var value routeValueArray
			if err := json.Unmarshal([]byte(raw), &value); err != nil {
				return "", err
			}
			return firstAliveNode(ctx, redisClient, value.Addresses), nil
		case "addr.v1":
			var value routeValueSingle
			if err := json.Unmarshal([]byte(raw), &value); err != nil {
				return "", err
			}
			if IsNodeAlive(ctx, redisClient, value.InstanceId) {
				return value.InstanceId, nil
			}
			return "", nil
		default:
			return "", fmt.Errorf("未知路由数据版本: %s", probe.Schema)
		}
	case '"':
		var addr string
		if err := json.Unmarshal([]byte(raw), &addr); err != nil {
			return "", err
		}
		if IsNodeAlive(ctx, redisClient, addr) {
			return addr, nil
		}
		return "", nil
	default:
		if IsNodeAlive(ctx, redisClient, raw) {
			return raw, nil
		}
		return "", nil
	}
}

func firstAliveNode(ctx context.Context, redisClient redis.Cmdable, addresses []string) string {
	for _, addr := range addresses {
		addr = strings.TrimSpace(addr)
		if IsNodeAlive(ctx, redisClient, addr) {
			return addr
		}
	}
	return ""
}
