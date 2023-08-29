package geerpc

import (
	"bufio"
	"errors"
	"fmt"
	"geerpc/codec"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
)

// Call 表示一个活跃的 RPC 调用。
type Call struct {
	Seq           uint64      // 调用序号
	ServiceMethod string      // 格式为 "<service>.<method>"
	Args          interface{} // 函数的参数
	Reply         interface{} // 函数的返回值
	Error         error       // 若出现错误，将被设置
	Done          chan *Call  // 在调用完成时发送信号
}

func (call *Call) done() {
	call.Done <- call
}

// Client 表示一个 RPC 客户端。
// 一个客户端可以有多个未完成的 Calls，且可以被多个 goroutine 同时使用。
type Client struct {
	cc       codec.Codec      // 编解码器
	opt      *Option          // 客户端选项
	sending  sync.Mutex       // 保护以下部分
	header   codec.Header     // 请求头
	mu       sync.Mutex       // 保护以下部分
	seq      uint64           // 调用序号
	pending  map[uint64]*Call // 未完成的调用
	closing  bool             // 用户调用了 Close
	shutdown bool             // 服务器告知停止
}

var _ io.Closer = (*Client)(nil)

var ErrShutdown = errors.New("connection is shut down")

// Close 关闭连接
func (client *Client) Close() error {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.closing {
		return ErrShutdown
	}
	client.closing = true
	return client.cc.Close()
}

// IsAvailable 如果客户端仍然可用，则返回 true
func (client *Client) IsAvailable() bool {
	client.mu.Lock()
	defer client.mu.Unlock()
	return !client.shutdown && !client.closing
}

// ...

// NewHTTPClient 通过 HTTP 连接创建一个 Client 实例
func NewHTTPClient(conn net.Conn, opt *Option) (*Client, error) {
	_, _ = io.WriteString(conn, fmt.Sprintf("CONNECT %s HTTP/1.0\n\n", defaultRPCPath))

	// 在切换到 RPC 协议之前，需要成功的 HTTP 响应。
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "CONNECT"})
	if err == nil && resp.Status == connected {
		return NewClient(conn, opt)
	}
	if err == nil {
		err = errors.New("unexpected HTTP response: " + resp.Status)
	}
	return nil, err
}

// DialHTTP 连接到指定网络地址的 HTTP RPC 服务器
func DialHTTP(network, address string, opts ...*Option) (*Client, error) {
	return dialTimeout(NewHTTPClient, network, address, opts...)
}

// XDial 根据第一个参数 rpcAddr 调用不同的函数来连接到 RPC 服务器
// rpcAddr 是一个通用格式（protocol@addr），用于表示 RPC 服务器
// 例如，http@10.0.0.1:7001，tcp@10.0.0.1:9999，unix@/tmp/geerpc.sock
func XDial(rpcAddr string, opts ...*Option) (*Client, error) {
	parts := strings.Split(rpcAddr, "@")
	if len(parts) != 2 {
		return nil, fmt.Errorf("rpc client err: wrong format '%s', expect protocol@addr", rpcAddr)
	}
	protocol, addr := parts[0], parts[1]
	switch protocol {
	case "http":
		return DialHTTP("tcp", addr, opts...)
	default:
		// tcp, unix 或其他传输协议
		return Dial(protocol, addr, opts...)
	}
}
