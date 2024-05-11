package serialization

import "sync"

var (
	serializations sync.Map //压缩类型
)

type Serialization interface {
	// Marshal
	//  @Description: 序列化数据
	//  @param interface{} 待序列化数据
	//  @return []byte 序列化后的数据
	//  @return error 错误信息
	Marshal(interface{}) ([]byte, error)
	// Unmarshal
	//  @Description: 反序列化数据
	//  @param []byte 待反序列化数据
	//  @param interface{} 反序列化后的数据
	//  @return error 错误信息
	Unmarshal([]byte, interface{}) error
	// MarshalAndCompression
	//  @Description: 序列化并压缩数据
	//  @param interface{}  待序列化压缩数据
	//  @return []byte 序列化压缩后的数据
	//  @return error 错误信息
	MarshalAndCompression(interface{}) ([]byte, error)
	// UnmarshalAndCompression
	//  @Description: 反序列化并解压数据
	//  @param []byte  待反序列化解压数据
	//  @return []byte 反序列化解压后的数据
	//  @return error 错误信息
	UnmarshalAndCompression([]byte, interface{}) error
}

func RegisterSerialization(serializationType string, serialization Serialization) {
	serializations.Store(serializationType, serialization)
}

func GetSerialization(serializationType string) Serialization {
	value, ok := serializations.Load(serializationType)
	if ok {
		return value.(Serialization)
	}
	return nil
}
