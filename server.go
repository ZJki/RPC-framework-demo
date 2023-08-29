// Package geerpc 实现了一个简单的 RPC 框架。
// 代码中包含了服务端、客户端和测试代码。
// 使用不同的编解码器（例如 JSON、Gob 等）实现了数据的传输和处理。
// 此代码遵循BSD-style许可证，版权归Go Authors所有。
package geerpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"geerpc/codec"
	"io"
	"log"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"
)

// MagicNumber 表示 geerpc 请求的幻数
const MagicNumber = 0x3bef5c

// Option 定义了 RPC 的选项
type Option struct {
	MagicNumber    int           // MagicNumber 用于标记这是一个 geerpc 请求
	CodecType      codec.Type    // 客户端可以选择不同的编解码器来编码请求体
	ConnectTimeout time.Duration // 0 表示没有超时限制
	HandleTimeout  time.Duration
}

// DefaultOption 是默认的 Option 实例
var DefaultOption = &Option{
	MagicNumber:    MagicNumber,
	CodecType:      codec.GobType,
	ConnectTimeout: time.Second * 10,
}

//令牌桶
type TokenBucket struct {
	tokens         int           // 当前令牌数量
	capacity       int           // 令牌桶容量
	refillAmount   int           // 每次填充的令牌数量
	refillInterval time.Duration // 填充间隔
	mu             sync.Mutex    // 互斥锁
	lastRefill     time.Time     // 上次填充时间
}

func NewTokenBucket(capacity, refillAmount int, refillInterval time.Duration) *TokenBucket {
	return &TokenBucket{
		tokens:         capacity,
		capacity:       capacity,
		refillAmount:   refillAmount,
		refillInterval: refillInterval,
		lastRefill:     time.Now(), // 初始设置为当前时间
	}
}

func (tb *TokenBucket) Allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	tokensToAdd := int(now.Sub(tb.lastRefill) / tb.refillInterval) * tb.refillAmount
	if tokensToAdd > 0 {
		tb.tokens = tb.tokens + tokensToAdd
		if tb.tokens > tb.capacity {
			tb.tokens = tb.capacity
		}
		tb.lastRefill = now
	}

	if tb.tokens > 0 {
		tb.tokens--
		return true
	}

	return false
}

// Server 表示一个 RPC 服务器
type Server struct {
	serviceMap sync.Map
}

// NewServer 返回一个新的 Server 实例
func NewServer() *Server {
	return &Server{}
}

// DefaultServer 是默认的 *Server 实例
var DefaultServer = NewServer()

// ServeConn 在单个连接上运行服务器，阻塞地为连接服务，直到客户端挂断
func (server *Server) ServeConn(conn io.ReadWriteCloser) {
	defer func() { _ = conn.Close() }()
	var opt Option
	if err := json.NewDecoder(conn).Decode(&opt); err != nil {
		log.Println("rpc server: options error: ", err)
		return
	}
	if opt.MagicNumber != MagicNumber {
		log.Printf("rpc server: invalid magic number %x", opt.MagicNumber)
		return
	}
	f := codec.NewCodecFuncMap[opt.CodecType]
	if f == nil {
		log.Printf("rpc server: invalid codec type %s", opt.CodecType)
		return
	}
	server.serveCodec(f(conn), &opt)
}

// invalidRequest 是发生错误时响应的占位符
var invalidRequest = struct{}{}

// serveCodec 处理编解码器并为请求提供服务
func (server *Server) serveCodec(cc codec.Codec, opt *Option) {
	sending := new(sync.Mutex) // 确保发送完整的响应
	wg := new(sync.WaitGroup)  // 等待所有请求处理完成
	tb := NewTokenBucket(10, 2, time.Second) // 创建令牌桶，每秒添加2个令牌
	for {
		// 检查令牌桶中是否有足够的令牌
		if !tb.Allow() {
			log.Println("rpc server: rate limit exceeded")
			// Send error response indicating rate limit exceeded
			server.sendResponse(cc, &codec.Header{ServiceMethod: ""}, "rate limit exceeded", sending)
			continue
		}
		req, err := server.readRequest(cc)
		if err != nil {
			if req == nil {
				break // 无法恢复，关闭连接
			}
			req.h.Error = err.Error()
			server.sendResponse(cc, req.h, invalidRequest, sending)
			continue
		}
		wg.Add(1)
		go server.handleRequest(cc, req, sending, wg, opt.HandleTimeout)
	}
	wg.Wait()
	_ = cc.Close()
}

// request 存储调用的所有信息
type request struct {
	h            *codec.Header // 请求的头部
	argv, replyv reflect.Value // 请求的参数和返回值
	mtype        *methodType
	svc          *service
}

// readRequestHeader 从编解码器中读取请求头部
func (server *Server) readRequestHeader(cc codec.Codec) (*codec.Header, error) {
	var h codec.Header
	if err := cc.ReadHeader(&h); err != nil {
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			log.Println("rpc server: read header error:", err)
		}
		return nil, err
	}
	return &h, nil
}

// findService 根据服务和方法名查找服务和方法类型
func (server *Server) findService(serviceMethod string) (svc *service, mtype *methodType, err error) {
	dot := strings.LastIndex(serviceMethod, ".")
	if dot < 0 {
		err = errors.New("rpc server: service/method request ill-formed: " + serviceMethod)
		return
	}
	serviceName, methodName := serviceMethod[:dot], serviceMethod[dot+1:]
	svci, ok := server.serviceMap.Load(serviceName)
	if !ok {
		err = errors.New("rpc server: can't find service " + serviceName)
		return
	}
	svc = svci.(*service)
	mtype = svc.method[methodName]
	if mtype == nil {
		err = errors.New("rpc server: can't find method " + methodName)
	}
	return
}

// readRequest 从编解码器中读取请求
func (server *Server) readRequest(cc codec.Codec) (*request, error) {
	h, err := server.readRequestHeader(cc)
	if err != nil {
		return nil, err
	}
	req := &request{h: h}
	req.svc, req.mtype, err = server.findService(h.ServiceMethod)
	if err != nil {
		return req, err
	}
	req.argv = req.mtype.newArgv()
	req.replyv = req.mtype.newReplyv()

	// 确保 argv 是指针，ReadBody 需要一个指针作为参数
	argvi := req.argv.Interface()
	if req.argv.Type().Kind() != reflect.Ptr {
		argvi = req.argv.Addr().Interface()
	}
	if err = cc.ReadBody(argvi); err != nil {
		log.Println("rpc server: read body err:", err)
		return req, err
	}
	return req, nil
}

// sendResponse 将响应发送给客户端
func (server *Server) sendResponse(cc codec.Codec, h *codec.Header, body interface{}, sending *sync.Mutex) {
	sending.Lock()
	defer sending.Unlock()
	if err := cc.Write(h, body); err != nil {
		log.Println("rpc server: write response error:", err)
	}
}

// handleRequest 处理请求
func (server *Server) handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup, timeout time.Duration) {
	defer wg.Done()
	called := make(chan struct{})
	sent := make(chan struct{})
	go func() {
		err := req.svc.call(req.mtype, req.argv, req.replyv)
		called <- struct{}{}
		if err != nil {
			req.h.Error = err.Error()
			server.sendResponse(cc, req.h, invalidRequest, sending)
			sent <- struct{}{}
			return
		}
		server.sendResponse(cc, req.h, req.replyv.Interface(), sending)
		sent <- struct{}{}
