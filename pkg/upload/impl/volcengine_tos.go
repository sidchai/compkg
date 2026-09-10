package impl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/sidchai/compkg/pkg/logger"
	"github.com/sidchai/compkg/pkg/upload"
	"github.com/volcengine/ve-tos-golang-sdk/v2/tos"
)

// 同一 endpoint+ak+region 复用 Client，避免每条上传新建连接池
var volcClientCache sync.Map

type VolcEngineTos struct {
	ETag            string
	FileSize        int64
	Catalogue       string
	IsTime          bool
	bucketName      string
	objectKey       string
	tosClient       *tos.ClientV2
	IsCustomStorage bool
	endpoint        string
}

func (v *VolcEngineTos) GetPresignedURL(path string) (string, error) {
	return "", nil
}

func init() {
	upload.RegisterOss("volcengine-tos", func() upload.Oss { return &VolcEngineTos{} })
}

func (v *VolcEngineTos) NewClient(ctx context.Context, opts ...upload.OssOption) {
	copyOpt := upload.DefaultOssOptions
	po := &copyOpt
	for _, opt := range opts {
		opt.Apply(po)
	}
	tosClient, err := NewClientV2(po.Endpoint, po.AccessKeyId, po.SecretAccessKey, po.BucketName)
	if err != nil {
		logger.Errorf("AliyunOss NewClient err:%+v", err.Error())
		return
	}
	v.tosClient = tosClient
	v.bucketName = po.BucketName
	v.objectKey = po.ObjectKey
	v.endpoint = po.Endpoint
}

func (v *VolcEngineTos) UploadFileLocal(fileName, fileLocalPath string) (string, error) {
	tosPath := fmt.Sprintf("%s/%s", v.Catalogue, fileName)
	var isObject bool
	if v.objectKey != "" {
		tosPath = fmt.Sprintf("%s/%s/%s", v.objectKey, v.Catalogue, fileName)
		isObject = true
	}
	if v.IsTime {
		if isObject {
			tosPath = fmt.Sprintf("%s/%s/%s/%s", v.objectKey, v.Catalogue, time.Now().Format("2006/01/02"), fileName)
		} else {
			tosPath = fmt.Sprintf("%s/%s/%s", v.Catalogue, time.Now().Format("2006/01/02"), fileName)
		}
	}
	if v.tosClient == nil {
		return "", errors.New("tosClient is nil")
	}
	f, err := os.Open(fileLocalPath)
	if err != nil {
		logger.Errorf("VolcEnginTos UploadFileLocal os.Open err:%+v", err.Error())
		return "", err
	}
	defer f.Close()
	fileInfo, err := f.Stat()
	if err != nil {
		return "", err
	}
	v.FileSize = fileInfo.Size()
	output, err := v.tosClient.PutObjectV2(context.Background(), &tos.PutObjectV2Input{
		PutObjectBasicInput: tos.PutObjectBasicInput{
			Bucket: v.bucketName,
			Key:    tosPath,
		},
		Content: f,
	})
	if err != nil {
		logger.Errorf("VolcEnginTos UploadFileLocal PutObjectV2 err:%+v", err.Error())
		return "", err
	}
	v.ETag = strings.Trim(output.ETag, `"`)
	return fmt.Sprintf("https://%s.%s/%s", v.bucketName, v.endpoint, tosPath), nil
}

func (v *VolcEngineTos) UploadFileIo(fileName string, content io.Reader) (string, error) {
	tosPath := fmt.Sprintf("%s/%s/%s", v.objectKey, v.Catalogue, fileName)
	if v.tosClient == nil {
		return "", errors.New("tosClient is nil")
	}
	_, err := v.tosClient.PutObjectV2(context.Background(), &tos.PutObjectV2Input{
		PutObjectBasicInput: tos.PutObjectBasicInput{
			Bucket: v.bucketName,
			Key:    tosPath,
		},
		Content: content,
	})
	if err != nil {
		logger.Errorf("VolcEnginTos UploadFileIo PutObjectV2 err:%+v", err.Error())
		return "", err
	}

	return fmt.Sprintf("https://%s.%s/%s", v.bucketName, v.endpoint, tosPath), nil
}

func (v *VolcEngineTos) GetETag() string {
	return v.ETag
}

func (v *VolcEngineTos) GetFileSize() int64 {
	return v.FileSize
}

func (v *VolcEngineTos) SetCatalogue(catalogue string) {
	v.Catalogue = catalogue
}

func (v *VolcEngineTos) SetIsTime(isTime bool) {
	v.IsTime = isTime
}

func (v *VolcEngineTos) SetCustomStorage(isCustomStorage bool) {
	v.IsCustomStorage = isCustomStorage
}

func (v *VolcEngineTos) Download(fileUrl, fileName, dataFolder string) error {
	return nil
}

func (v *VolcEngineTos) PutACL(path string) error {
	return nil
}

// CopySelf 火山引擎 TOS 暂未实现深度归档复制（保留接口兼容）
func (v *VolcEngineTos) CopySelf(path string, storageClass string) error {
	return nil
}

func (v *VolcEngineTos) SetTagging(path string, tags map[string]string) error {
	return nil
}

// Delete 删除火山引擎 TOS 对象
// path 为上传时返回的 voldtos://{bucket}/{key}，需剥离前缀得到 key
func (v *VolcEngineTos) Delete(path string) error {
	if v.tosClient == nil {
		return errors.New("tosClient is nil")
	}
	key := strings.TrimPrefix(path, fmt.Sprintf("voldtos://%s/", v.bucketName))
	if _, err := v.tosClient.DeleteObjectV2(context.Background(), &tos.DeleteObjectV2Input{
		Bucket: v.bucketName,
		Key:    key,
	}); err != nil {
		logger.Errorf("VolcEngineTos Delete DeleteObjectV2 err:%+v", err.Error())
		return err
	}
	return nil
}

func NewClientV2(endpoint, accessKey, secretKey, region string) (*tos.ClientV2, error) {
	cacheKey := endpoint + "\x00" + accessKey + "\x00" + region
	if cached, ok := volcClientCache.Load(cacheKey); ok {
		return cached.(*tos.ClientV2), nil
	}
	tosClient, err := tos.NewClientV2(endpoint,
		tos.WithRegion(region),
		tos.WithCredentials(tos.NewStaticCredentials(accessKey, secretKey)),
		tos.WithMaxConnections(256),
		tos.WithIdleConnTimeout(90*time.Second),
	)
	if err != nil {
		hlog.Errorf("VolcEngineTos NewClientV2 err:%+v", err.Error())
		return nil, err
	}
	actual, _ := volcClientCache.LoadOrStore(cacheKey, tosClient)
	return actual.(*tos.ClientV2), nil
}
