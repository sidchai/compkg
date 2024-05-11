package compression

import "sync"

var (
	compressions sync.Map //压缩类型
)

type Compression interface {
	// Compress
	//  @Description: 压缩数据
	//  @param []byte 待压缩数据
	//  @return []byte 压缩完数据
	//  @return error 错误信息
	Compress([]byte) ([]byte, error)
	// Decompress
	//  @Description: 解压数据
	//  @param []byte 待解压数据
	//  @return []byte 解压完数据
	//  @return error 错误信息
	Decompress([]byte) ([]byte, error)
}

func RegisterCompression(compressionType string, compression Compression) {
	compressions.Store(compressionType, compression)
}

func GetCompression(compressionType string) Compression {
	value, ok := compressions.Load(compressionType)
	if ok {
		return value.(Compression)
	}
	return nil
}
