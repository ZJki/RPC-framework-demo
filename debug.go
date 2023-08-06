// Package geerpc 实现了一个简单的 RPC 框架。
// 代码中包含了服务端、客户端和测试代码。
// 使用不同的编解码器（例如 JSON、Gob 等）实现了数据的传输和处理。
// 此代码遵循BSD-style许可证，版权归Go Authors所有。
package geerpc

import (
	"fmt"
	"html/template"
	"net/http"
)

// debugText 是用于展示调试信息的 HTML 模板
const debugText = `<html>
	<body>
	<title>GeeRPC Services</title>
	{{range .}}
	<hr>
	Service {{.Name}}
	<hr>
		<table>
		<th align=center>Method</th><th align=center>Calls</th>
		{{range $name, $mtype := .Method}}
			<tr>
			<td align=left font=fixed>{{$name}}({{$mtype.ArgType}}, {{$mtype.ReplyType}}) error</td>
			<td align=center>{{$mtype.NumCalls}}</td>
			</tr>
		{{end}}
		</table>
	{{end}}
	</body>
	</html>`

// debug 是调试模板的实例
var debug = template.Must(template.New("RPC debug").Parse(debugText))

// debugHTTP 是用于调试的 HTTP 处理器
type debugHTTP struct {
	*Server
}

// debugService 存储调试信息的结构体
type debugService struct {
	Name   string
	Method map[string]*methodType
}

// ServeHTTP 在 /debug/geerpc 上运行调试服务
func (server debugHTTP) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// 构建一个排序后的数据版本
	var services []debugService
	server.serviceMap.Range(func(namei, svci interface{}) bool {
		svc := svci.(*service)
		services = append(services, debugService{
			Name:   namei.(string),
			Method: svc.method,
		})
		return true
	})
	err := debug.Execute(w, services)
	if err != nil {
		_, _ = fmt.Fprintln(w, "rpc: error executing template:", err.Error())
	}
}
