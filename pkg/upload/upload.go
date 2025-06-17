package upload

import (
	"context"
	"io"
)

var (
	platforms map[string]Oss //平台类型
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
}

func init() {
	platforms = make(map[string]Oss)
}

func RegisterOss(platformType string, platform Oss) {
	platforms[platformType] = platform
}

func GetPlatform(platformType string) Oss {
	platform, ok := platforms[platformType]
	if ok {
		return platform
	}
	return nil
}
