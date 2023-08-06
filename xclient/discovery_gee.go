package xclient

import (
	"log"
	"net/http"
	"strings"
	"time"
)

// GeeRegistryDiscovery 是一个基于 GeeRegistry 的服务发现实现
type GeeRegistryDiscovery struct {
	*MultiServersDiscovery
	registry   string        // 注册中心地址
	timeout    time.Duration // 刷新超时时间
	lastUpdate time.Time     // 上次刷新时间
}

const defaultUpdateTimeout = time.Second * 10

// Update 更新服务器列表
func (d *GeeRegistryDiscovery) Update(servers []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.servers = servers
	d.lastUpdate = time.Now()
	return nil
}

// Refresh 从注册中心刷新服务器列表
func (d *GeeRegistryDiscovery) Refresh() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	// 如果上次刷新时间距离现在超过超时时间，则进行刷新
	if d.lastUpdate.Add(d.timeout).After(time.Now()) {
		return nil
	}
	log.Println("rpc registry: refresh servers from registry", d.registry)
	resp, err := http.Get(d.registry)
	if err != nil {
		log.Println("rpc registry refresh err:", err)
		return err
	}
	servers := strings.Split(resp.Header.Get("X-Geerpc-Servers"), ",")
	d.servers = make([]string, 0, len(servers))
	for _, server := range servers {
		if strings.TrimSpace(server) != "" {
			d.servers = append(d.servers, strings.TrimSpace(server))
		}
	}
	d.lastUpdate = time.Now()
	return nil
}

// Get 根据选择模式从服务器列表中选择一个服务器
func (d *GeeRegistryDiscovery) Get(mode SelectMode) (string, error) {
	if err := d.Refresh(); err != nil {
		return "", err
	}
	return d.MultiServersDiscovery.Get(mode)
}

// GetAll 返回所有服务器列表
func (d *GeeRegistryDiscovery) GetAll() ([]string, error) {
	if err := d.Refresh(); err != nil {
		return nil, err
	}
	return d.MultiServersDiscovery.GetAll()
}

// NewGeeRegistryDiscovery 创建一个 GeeRegistryDiscovery 实例
func NewGeeRegistryDiscovery(registerAddr string, timeout time.Duration) *GeeRegistryDiscovery {
	if timeout == 0 {
		timeout = defaultUpdateTimeout
	}
	d := &GeeRegistryDiscovery{
		MultiServersDiscovery: NewMultiServerDiscovery(make([]string, 0)),
		registry:              registerAddr,
		timeout:               timeout,
	}
	return d
}
