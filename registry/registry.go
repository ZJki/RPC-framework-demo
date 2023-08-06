package registry

import (
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// GeeRegistry 是一个简单的注册中心，提供以下功能：
// 添加一个服务器并接收心跳以保持其活动状态。
// 同步同时返回所有活动服务器并删除失效的服务器。
type GeeRegistry struct {
	timeout time.Duration
	mu      sync.Mutex // 保护以下字段
	servers map[string]*ServerItem
}

// ServerItem 记录服务器的信息
type ServerItem struct {
	Addr  string
	start time.Time
}

const (
	defaultPath    = "/_geerpc_/registry"
	defaultTimeout = time.Minute * 5
)

// New 创建一个带有超时设置的注册中心实例
func New(timeout time.Duration) *GeeRegistry {
	return &GeeRegistry{
		servers: make(map[string]*ServerItem),
		timeout: timeout,
	}
}

var DefaultGeeRegister = New(defaultTimeout)

// putServer 将服务器添加到注册中心或更新其活动时间
func (r *GeeRegistry) putServer(addr string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.servers[addr]
	if s == nil {
		r.servers[addr] = &ServerItem{Addr: addr, start: time.Now()}
	} else {
		s.start = time.Now() // 如果已存在，更新活动时间以保持活跃
	}
}

// aliveServers 返回所有活动服务器的地址
func (r *GeeRegistry) aliveServers() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var alive []string
	for addr, s := range r.servers {
		if r.timeout == 0 || s.start.Add(r.timeout).After(time.Now()) {
			alive = append(alive, addr)
		} else {
			delete(r.servers, addr)
		}
	}
	sort.Strings(alive)
	return alive
}

// ServeHTTP 处理 HTTP 请求，返回活动服务器列表或接收服务器的心跳
func (r *GeeRegistry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case "GET":
		// 简化起见，服务器列表在 req.Header 中
		w.Header().Set("X-Geerpc-Servers", strings.Join(r.aliveServers(), ","))
	case "POST":
		// 简化起见，服务器地址在 req.Header 中
		addr := req.Header.Get("X-Geerpc-Server")
		if addr == "" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		r.putServer(addr)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// HandleHTTP 在 registryPath 上注册 GeeRegistry 的 HTTP 处理程序
func (r *GeeRegistry) HandleHTTP(registryPath string) {
	http.Handle(registryPath, r)
	log.Println("rpc registry path:", registryPath)
}

// HandleHTTP 注册默认路径的 HTTP 处理程序
func HandleHTTP() {
	DefaultGeeRegister.HandleHTTP(defaultPath)
}

// Heartbeat 定期发送心跳消息
// 作为服务器注册或发送心跳的辅助函数
func Heartbeat(registry, addr string, duration time.Duration) {
	if duration == 0 {
		// 确保在从注册中心移除之前有足够的时间发送心跳
		duration = defaultTimeout - time.Duration(1)*time.Minute
	}
	var err error
	err = sendHeartbeat(registry, addr)
	go func() {
		t := time.NewTicker(duration)
		for err == nil {
			<-t.C
			err = sendHeartbeat(registry, addr)
		}
	}()
}

func sendHeartbeat(registry, addr string) error {
	log.Println(addr, "send heart beat to registry", registry)
	httpClient := &http.Client{}
	req, _ := http.NewRequest("POST", registry, nil)
	req.Header.Set("X-Geerpc-Server", addr)
	if _, err := httpClient.Do(req); err != nil {
		log.Println("rpc server: heart beat err:", err)
		return err
	}
	return nil
}
