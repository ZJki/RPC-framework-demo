package xclient

import (
	"errors"
	"math"
	"math/rand"
	"sync"
	"time"
)

// SelectMode 定义了服务选择的模式
type SelectMode int

const (
	RandomSelect     SelectMode = iota // 随机选择
	RoundRobinSelect                   // 轮询选择
)

// Discovery 是一个服务发现的接口，用于获取可用的服务器列表
type Discovery interface {
	Refresh() error // 刷新服务器列表
	Update(servers []string) error
	Get(mode SelectMode) (string, error)
	GetAll() ([]string, error)
}

var _ Discovery = (*MultiServersDiscovery)(nil)

// MultiServersDiscovery 是一个没有注册中心的多服务器发现实现
// 用户需要显式提供服务器地址
type MultiServersDiscovery struct {
	r       *rand.Rand   // 用于生成随机数
	mu      sync.RWMutex // 保护以下字段
	servers []string
	index   int // 记录轮询算法选择的位置
}

// Refresh 对 MultiServersDiscovery 来说没有意义，因此忽略它
func (d *MultiServersDiscovery) Refresh() error {
	return nil
}

// Update 动态更新发现实例的服务器列表
func (d *MultiServersDiscovery) Update(servers []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.servers = servers
	return nil
}

// Get 根据选择模式获取一个服务器
func (d *MultiServersDiscovery) Get(mode SelectMode) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := len(d.servers)
	if n == 0 {
		return "", errors.New("rpc discovery: no available servers")
	}
	switch mode {
	case RandomSelect:
		return d.servers[d.r.Intn(n)], nil
	case RoundRobinSelect:
		s := d.servers[d.index%n] // 服务器列表可能已更新，使用取模 n 确保安全性
		d.index = (d.index + 1) % n
		return s, nil
	default:
		return "", errors.New("rpc discovery: not supported select mode")
	}
}

// GetAll 返回发现实例中的所有服务器
func (d *MultiServersDiscovery) GetAll() ([]string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	// 返回 d.servers 的副本
	servers := make([]string, len(d.servers), len(d.servers))
	copy(servers, d.servers)
	return servers, nil
}

// NewMultiServerDiscovery 创建一个 MultiServersDiscovery 实例
func NewMultiServerDiscovery(servers []string) *MultiServersDiscovery {
	d := &MultiServersDiscovery{
		servers: servers,
		r:       rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	d.index = d.r.Intn(math.MaxInt32 - 1)
	return d
}
