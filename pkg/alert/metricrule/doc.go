// Package metricrule 是旧 YAML 埋点规则引擎（iot_server 12 条 + audio 29 条）。
//
// 不能删、也不能和 P0 门面合成同一个 package：
// 两边都有 Config、LevelP0，一个是 int 级别，一个是 string 事件码。
// iot_server / audio_self_host 继续 Init 本包。出口实现在 notifier，Webhook 与 P0 共用 6.3 JSON。
package metricrule
