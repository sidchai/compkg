package upload

import (
	"context"
	"io"
)

// OssFactory 创建Oss实例的工厂函数
type OssFactory func() Oss

var (
	platforms map[string]OssFactory //平台类型工厂
)

// Oss
//
//	@Description: 对象存储接口
type Oss interface {
	NewClient(ctx context.Context, opts ...OssOption)                // 实例化
	UploadFileLocal(fileName, fileLocalPath string) (string, error)  // 上传本地文件
	UploadFileIo(fileName string, content io.Reader) (string, error) // 上传文件流数据
	GetETag() string                                                 // 获取文件md5校验值
	GetFileSize() int64                                              // 获取文件大小
	Download(fileUrl, fileName, dataFolder string) error             // 下载文件
	PutACL(path string) error                                        // 设置文件参数
	SetCatalogue(catalogue string)                                   // 设置存储目录
	SetIsTime(isTime bool)                                           // 是否添加时间
	SetTagging(path string, tags map[string]string) error            // 设置标签
	SetCustomStorage(isCustomStorage bool)                           // 设置是否自定义存储
	GetPresignedURL(path string) (string, error)                     // 获取预签名URL
}

func init() {
	platforms = make(map[string]OssFactory)
}

// RegisterOss 注册Oss工厂函数
func RegisterOss(platformType string, factory OssFactory) {
	platforms[platformType] = factory
}

// GetPlatform 获取新的Oss实例（每次调用返回新实例，避免并发问题）
func GetPlatform(platformType string) Oss {
	factory, ok := platforms[platformType]
	if ok {
		return factory()
	}
	return nil
}
