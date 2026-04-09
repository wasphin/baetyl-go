package v1

// MessageKind message kind
type MessageKind string

const (
	// Message response message kind
	MessageWebsocketRead MessageKind = "wsReadMsg"
	// MessageReport report message kind
	MessageReport MessageKind = "report"
	// MessageDesire desire message kind
	MessageDesire MessageKind = "desire"
	// MessageKeep keep alive message kind
	MessageKeep MessageKind = "keepalive"
	// MessageCMD command message kind
	MessageCMD MessageKind = "cmd"
	// MessageData data message kind
	MessageData MessageKind = "data"
	// MessageError error message kind
	MessageError MessageKind = "error"
	// Message response message kind
	MessageResponse MessageKind = "response"
	// MessageDelta delta message kind
	MessageDelta MessageKind = "delta"
	// MessageEvent event message = "event"
	MessageEvent MessageKind = "event"
	// MessageNodeProps node props message kind
	MessageNodeProps MessageKind = "nodeProps"
	// MessageDevices devices message kind
	MessageDevices MessageKind = "devices"
	// MessageDeviceEvent device event message kind
	MessageDeviceEvent MessageKind = "deviceEvent"
	// MessageReport device report message kind
	MessageDeviceReport MessageKind = "deviceReport"
	// MessageDesire device desire message kind
	MessageDeviceDesire MessageKind = "deviceDesire"
	//new version MessageDesire device desire message kind
	MessageMultipleDeviceDesire MessageKind = "multipleDeviceDesire"
	// MessageDesire device delta message kind
	MessageDeviceDelta MessageKind = "deviceDelta"
	// MessageDeviceLatestProperty device get property message kind
	MessageDevicePropertyGet MessageKind = "thing.property.get"
	// MessageReport device event report message kind
	MessageDeviceEventReport MessageKind = "thing.event.post"
	// MessageReport device lifecycle report message kind
	MessageDeviceLifecycleReport MessageKind = "thing.lifecycle.post"
	// MessageMC get/send mc info
	MessageMC MessageKind = "mc"
	// MessageSTS get/send s3 sts info
	MessageSTS MessageKind = "sts"

	// MessageCommandConnect start remote debug command
	MessageCommandConnect = "connect"
	// MessageCommandProxy start remote proxy command, cloud -> edge
	MessageCommandProxy = "proxy"
	// MessageCommandDisconnect stop remote debug/proxy command
	MessageCommandDisconnect = "disconnect"
	// MessageCommandLogs logs
	MessageCommandLogs = "logs"
	// MessageCommandNodeLabel label the edge cluster nodes
	MessageCommandNodeLabel = "nodeLabel"
	// MessageCommandMultiNodeLabels label multiple nodes
	MessageCommandMultiNodeLabels = "multiNodeLabels"
	// MessageCommandDescribe describe
	MessageCommandDescribe = "describe"
	// MessageRPC call edge app
	MessageRPC = "rpc"
	// MessageAgent get or set agent stat
	MessageAgent = "agent"
	// MessageRPCMqtt push message to broker
	MessageRPCMqtt = "rpcMqtt"
	// MessageMCAppList get mc app list
	MessageMCAppList = "mcAppList"
	// MessageMCAppInfo get mc app info
	MessageMCAppInfo = "mcAppInfo"
	// MessageSTSTypeMinio get minio sts
	MessageSTSTypeMinio = "minio"
)

// Message general structure for http and ws sync
type Message struct {
	Kind     MessageKind       `yaml:"kind" json:"kind"`
	Metadata map[string]string `yaml:"meta" json:"meta"`
	Content  LazyValue         `yaml:"content" json:"content"`
}

// PartitionKey 返回消息的分区键，用于将消息路由到固定 worker 以保证有序处理。
// 相同 key 的消息由同一个 worker 串行处理；不同 key 的消息可并发处理。
func (m *Message) PartitionKey() string {
	switch m.Kind {
	case MessageDevices:
		// 设备列表变更：全局串行（同一 worker）
		// 原因：该消息会触发 reSubscribe，影响所有设备，必须与其他设备消息严格有序
		return string(m.Kind)

	case MessageDeviceEvent, MessageDevicePropertyGet, MessageDeviceDelta:
		// 设备属性/事件消息：按设备名分区，同一设备内有序，不同设备并发
		if device := m.Metadata["device"]; device != "" {
			return device
		}
		return string(m.Kind)

	case MessageCMD, MessageData:
		// 远程调试 session 消息：按 session 标识分区
		// key 与 engine/msg_handler.go 中 `key` 变量构造方式一致
		return m.Metadata["namespace"] + "_" +
			m.Metadata["name"] + "_" +
			m.Metadata["container"] + "_" +
			m.Metadata["token"]

	default:
		// 其他消息按 Kind 分区，同类型消息串行，保守兜底
		return string(m.Kind)
	}
}
