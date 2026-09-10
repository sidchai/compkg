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

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/sidchai/compkg/pkg/logger"
	"github.com/sidchai/compkg/pkg/upload"
	"github.com/sidchai/compkg/pkg/util"
)

// 同一 endpoint+ak+bucket 复用 Client，否则每条上传都新建连接池，100 并发会打满握手而不是打满带宽
var aliyunBucketCache sync.Map

type AliyunOss struct {
	ETag            string
	FileSize        int64
	Catalogue       string
	IsTime          bool
	ossBucket       *oss.Bucket
	bucketName      string
	endpoint        string
	IsCustomStorage bool
	expires         int64
}

func init() {
	upload.RegisterOss("aliyun-oss", func() upload.Oss { return &AliyunOss{} })
}

func (a *AliyunOss) GetPresignedURL(path string) (string, error) {
	key := strings.ReplaceAll(path, fmt.Sprintf("https://%s.%s/", a.bucketName, a.endpoint), "")
	return a.ossBucket.SignURL(key, oss.HTTPGet, a.expires)
}

func (a *AliyunOss) NewClient(ctx context.Context, opts ...upload.OssOption) {
	copyOpt := upload.DefaultOssOptions
	po := &copyOpt
	for _, opt := range opts {
		opt.Apply(po)
	}
	bucket, err := NewBucket(po.Endpoint, po.AccessKeyId, po.SecretAccessKey, po.BucketName)
	if err != nil {
		logger.Errorf("AliyunOss NewClient err:%+v", err.Error())
		return
	}
	a.ossBucket = bucket
	a.bucketName = po.BucketName
	a.endpoint = po.Endpoint
	a.expires = po.Expires
}

func (a *AliyunOss) UploadFileLocal(fileName, fileLocalPath string) (string, error) {
	if a.ossBucket == nil {
		return "", errors.New("ossBucket is nil")
	}
	if a.Catalogue == "" {
		a.Catalogue = "audio"
	}
	ossPath := fmt.Sprintf("%s/%s", a.Catalogue, fileName)
	if a.IsTime {
		ossPath = fmt.Sprintf("%s/%s/%s", a.Catalogue, time.Now().Format("2006/01/02"), fileName)
	}
	if a.IsCustomStorage {
		ossPath = strings.ReplaceAll(fileName, fmt.Sprintf("https://%s.%s/", a.bucketName, a.endpoint), "")
	}
	fd, err := os.Open(fileLocalPath)
	if err != nil {
		return "", err
	}
	defer fd.Close()
	fi, err := fd.Stat()
	if err != nil {
		return "", err
	}
	a.FileSize = fi.Size()
	// 用 Put 响应头里的 ETag（简单上传即为文件 MD5），避免上传后再整文件读一遍算哈希
	resp, err := a.ossBucket.DoPutObject(&oss.PutObjectRequest{ObjectKey: ossPath, Reader: fd}, nil)
	if err != nil {
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		logger.Errorf("AliyunOss UploadFileLocal PutObject err:%+v", err.Error())
		return "", err
	}
	defer resp.Body.Close()
	a.ETag = strings.Trim(resp.Headers.Get("ETag"), `"`)
	return fmt.Sprintf("https://%s.%s/%s", a.bucketName, a.endpoint, ossPath), nil
}

func (a *AliyunOss) UploadFileIo(fileName string, content io.Reader) (string, error) {
	if a.ossBucket == nil {
		return "", errors.New("ossBucket is nil")
	}
	if a.Catalogue == "" {
		a.Catalogue = "audio"
	}
	ossPath := fmt.Sprintf("%s/%s", a.Catalogue, fileName)
	if a.IsTime {
		ossPath = fmt.Sprintf("%s/%s/%s", a.Catalogue, time.Now().Format("2006/01/02"), fileName)
	}
	if a.IsCustomStorage {
		ossPath = strings.ReplaceAll(fileName, fmt.Sprintf("https://%s.%s/", a.bucketName, a.endpoint), "")
	}
	if err := a.ossBucket.PutObject(ossPath, content); err != nil {
		logger.Errorf("AliyunOss UploadFileIo PutObject err:%+v", err.Error())
		return "", nil
	}
	return fmt.Sprintf("https://%s.%s/%s", a.bucketName, a.endpoint, ossPath), nil
}

func (a *AliyunOss) GetETag() string {
	return a.ETag
}

func (a *AliyunOss) GetFileSize() int64 {
	return a.FileSize
}

func (a *AliyunOss) SetCatalogue(catalogue string) {
	a.Catalogue = catalogue
}

func (a *AliyunOss) SetIsTime(isTime bool) {
	a.IsTime = isTime
}

func (a *AliyunOss) SetCustomStorage(isCustomStorage bool) {
	a.IsCustomStorage = isCustomStorage
}

func (a *AliyunOss) Download(fileUrl, fileName, dataFolder string) error {
	key := strings.ReplaceAll(fileUrl, fmt.Sprintf("https://%s.%s/", a.bucketName, a.endpoint), "")
	if !util.CheckFolder(dataFolder) {
		err := os.MkdirAll(dataFolder, os.ModePerm)
		if err != nil {
			logger.Info("创建目录异常 -> %v", err)
		} else {
			logger.Info("创建成功!")
		}
	}
	file := fmt.Sprintf("%s%s", dataFolder, fileName)
	err := a.ossBucket.GetObjectToFile(key, file)
	if err != nil {
		logger.Error("AliyunOss Download GetObjectToFile err:%v", err.Error())
		return err
	}
	return nil
}

func (a *AliyunOss) PutACL(path string) error {
	err := a.ossBucket.SetObjectACL(path, oss.ACLPrivate)
	if err != nil {
		return err
	}
	return nil
}

// CopySelf 阿里云 OSS 暂未实现深度归档复制（保留接口兼容）
func (a *AliyunOss) CopySelf(path string, storageClass string) error {
	return nil
}

// Delete 删除阿里云 OSS 对象
// path 为完整外链 URL，需先剥离 https://{bucket}.{endpoint}/ 前缀得到 objectKey
func (a *AliyunOss) Delete(path string) error {
	if a.ossBucket == nil {
		return errors.New("ossBucket is nil")
	}
	key := strings.ReplaceAll(path, fmt.Sprintf("https://%s.%s/", a.bucketName, a.endpoint), "")
	if err := a.ossBucket.DeleteObject(key); err != nil {
		logger.Errorf("AliyunOss Delete DeleteObject err:%+v", err.Error())
		return err
	}
	return nil
}

func (a *AliyunOss) SetTagging(path string, tags map[string]string) error {
	tagSet := make([]oss.Tag, 0)
	for k, v := range tags {
		tagSet = append(tagSet, oss.Tag{
			Key:   k,
			Value: v,
		})
	}

	// 设置标签
	opt := oss.Tagging{
		Tags: tagSet,
	}
	err := a.ossBucket.PutObjectTagging(path, opt)
	if err != nil {
		return err
	}
	return nil
}

func NewBucket(endpoint, accessKeyId, accessKeySecret, bucketName string) (*oss.Bucket, error) {
	cacheKey := endpoint + "\x00" + accessKeyId + "\x00" + bucketName
	if cached, ok := aliyunBucketCache.Load(cacheKey); ok {
		return cached.(*oss.Bucket), nil
	}
	// MaxConns 必须盖过转存并发，否则连接排队会表现为“上传很慢”
	client, err := oss.New(endpoint, accessKeyId, accessKeySecret, oss.MaxConns(512, 256, 256))
	if err != nil {
		hlog.Errorf("AliyunOss NewBucket err:%+v", err.Error())
		return nil, err
	}
	logger.Infof("bucketName:%s", bucketName)
	bucket, err := client.Bucket(bucketName)
	if err != nil {
		logger.Errorf("AliyunOss get Bucket err:%+v", err.Error())
		return nil, err
	}
	actual, _ := aliyunBucketCache.LoadOrStore(cacheKey, bucket)
	return actual.(*oss.Bucket), nil
}
