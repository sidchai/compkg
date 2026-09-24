// Package alert 是三代 IoT P0 告警的对外门面。
//
// 业务只依赖本包：Ingest / MarkStart / MarkEnd / Enqueue。
// 旧 YAML 埋点规则引擎在 alert/metricrule，不要和本包 Config / Event 混用：
// 两边都有 Config、LevelP0，类型完全不同。
// 实现按限界上下文放在 internal 中，禁止五服务直接引用 internal。
//
//	internal/core      领域模型与端口
//	internal/sender    Webhook / 钉钉适配器
//	internal/reporter  有界异步发送
//	internal/engine    状态机、抑制、指令聚合
//	internal/store     本机 StateStore
//	internal/pipeline  阶段交接
//	internal/ingest    非阻塞采样
//	internal/device    设备心跳引擎
package alert
