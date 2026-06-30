package traffic

import (
	"net"
	"sync/atomic"
)

// TrackedConn 流量追踪 Conn 包装器，实现 net.Conn。
//
// 工作模式：
//   - Read 上行（设备 → 服务）
//   - Write 下行（服务 → 设备）
//   - deviceNo 为空时不统计（零开销）
//   - deviceNo 非空但不在白名单时也不会真正累加（IsTracked 判断在底层 Add 函数）
//
// 典型用法：
//
//	conn = traffic.Wrap(conn)            // accept 后立即包装
//	conn.SetDeviceNo(parsedDeviceNo)     // 握手解析到设备号后激活
//	// 之后所有 conn.Read / Write 自动统计
type TrackedConn struct {
	net.Conn
	deviceNo atomic.Value // 存 string，可在握手后通过 SetDeviceNo 设置
}

// newTrackedConn 包内构造，业务层调用 Wrap()。
func newTrackedConn(conn net.Conn) *TrackedConn {
	tc := &TrackedConn{Conn: conn}
	tc.deviceNo.Store("")
	return tc
}

// SetDeviceNo 设置当前 conn 关联的设备号。
// 通常在 TCP 握手或第一条业务报文解析出设备号后调用。
// 设备号若不在白名单，后续 Read/Write 也不会真正累加（在 AddUp/AddDown 内做白名单判断）。
// 传空字符串无效（避免误清除）。
func (c *TrackedConn) SetDeviceNo(no string) {
	if no == "" {
		return
	}
	c.deviceNo.Store(no)
}

// DeviceNo 返回当前关联的设备号；未设置返回 ""。
func (c *TrackedConn) DeviceNo() string {
	if v := c.deviceNo.Load(); v != nil {
		return v.(string)
	}
	return ""
}

// Read 包装 net.Conn.Read，按返回字节数累加上行流量。
func (c *TrackedConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if n > 0 {
		if no := c.DeviceNo(); no != "" {
			AddUp(no, n)
		}
	}
	return n, err
}

// Write 包装 net.Conn.Write，按返回字节数累加下行流量。
func (c *TrackedConn) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	if n > 0 {
		if no := c.DeviceNo(); no != "" {
			AddDown(no, n)
		}
	}
	return n, err
}
