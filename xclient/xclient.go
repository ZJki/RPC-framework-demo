package xclient

import (
	"context"
	. "geerpc" // 引入 geerpc 包
	"io"
	"reflect"
	"sync"
)

// XClient 定义了一个支持负载均衡的 RPC 客户端
type XClient struct {
	d       Discovery
	mode    SelectMode
	opt     *Option
	mu      sync.Mutex // 用于保护以下字段
	clients map[string]*Client
}

// 实现 io.Closer 接口
var _ io.Closer = (*XClient)(nil)

// NewXClient 创建一个新的 XClient 实例
func NewXClient(d Discovery, mode SelectMode, opt *Option) *XClient {
	return &XClient{d: d, mode: mode, opt: opt, clients: make(map[string]*Client)}
}

// Close 关闭 XClient，释放底层的客户端连接
func (xc *XClient) Close() error {
	xc.mu.Lock()
	defer xc.mu.Unlock()
	for key, client := range xc.clients {
		// 忽略错误，关闭客户端连接
		_ = client.Close()
		delete(xc.clients, key)
	}
	return nil
}

// dial 根据给定的 RPC 地址创建一个客户端连接
func (xc *XClient) dial(rpcAddr string) (*Client, error) {
	xc.mu.Lock()
	defer xc.mu.Unlock()
	client, ok := xc.clients[rpcAddr]
	if ok && !client.IsAvailable() {
		_ = client.Close()
		delete(xc.clients, rpcAddr)
		client = nil
	}
	if client == nil {
		var err error
		client, err = XDial(rpcAddr, xc.opt)
		if err != nil {
			return nil, err
		}
		xc.clients[rpcAddr] = client
	}
	return client, nil
}

// call 调用指定的服务方法
func (xc *XClient) call(rpcAddr string, ctx context.Context, serviceMethod string, args, reply interface{}) error {
	client, err := xc.dial(rpcAddr)
	if err != nil {
		return err
	}
	return client.Call(ctx, serviceMethod, args, reply)
}

// Call 调用指定的服务方法，XClient 会选择一个合适的服务器进行调用
func (xc *XClient) Call(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	rpcAddr, err := xc.d.Get(xc.mode)
	if err != nil {
		return err
	}
	return xc.call(rpcAddr, ctx, serviceMethod, args, reply)
}

// Broadcast 对注册在发现服务中的所有服务器调用指定的服务方法
func (xc *XClient) Broadcast(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	servers, err := xc.d.GetAll()
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	var mu sync.Mutex // 保护 e 和 replyDone
	var e error
	replyDone := reply == nil // 如果 reply 为 nil，则无需设置值
	ctx, cancel := context.WithCancel(ctx)
	for _, rpcAddr := range servers {
		wg.Add(1)
		go func(rpcAddr string) {
			defer wg.Done()
			var clonedReply interface{}
			if reply != nil {
				clonedReply = reflect.New(reflect.ValueOf(reply).Elem().Type()).Interface()
			}
			err := xc.call(rpcAddr, ctx, serviceMethod, args, clonedReply)
			mu.Lock()
			if err != nil && e == nil {
				e = err
				cancel() // 如果任何调用失败，取消未完成的调用
			}
			if err == nil && !replyDone {
				reflect.ValueOf(reply).Elem().Set(reflect.ValueOf(clonedReply).Elem())
				replyDone = true
			}
			mu.Unlock()
		}(rpcAddr)
	}
	wg.Wait()
	return e
}
