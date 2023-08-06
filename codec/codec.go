package codec

import (
	"io"
)

// Header 是消息头的结构体，包含服务方法名、序列号和错误信息
type Header struct {
	ServiceMethod string // 格式为 "Service.Method"
	Seq           uint64 // 客户端选择的序列号
	Error         string
}

// Codec 定义了编解码器的接口
type Codec interface {
	io.Closer
	ReadHeader(*Header) error
	ReadBody(interface{}) error
	Write(*Header, interface{}) error
}

// NewCodecFunc 是用于创建 Codec 实例的函数类型
type NewCodecFunc func(io.ReadWriteCloser) Codec

// Type 是编解码器的类型
type Type string

const (
	GobType  Type = "application/gob"
	JsonType Type = "application/json" // 未实现
)

// NewCodecFuncMap 存储不同类型的编解码器创建函数
var NewCodecFuncMap map[Type]NewCodecFunc

func init() {
	NewCodecFuncMap = make(map[Type]NewCodecFunc)
	NewCodecFuncMap[GobType] = NewGobCodec
}
