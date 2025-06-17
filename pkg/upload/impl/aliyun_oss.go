package impl

import (
	"context"
	"errors"
	"fmt"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/sidchai/compkg/pkg/logger"
	"github.com/sidchai/compkg/pkg/upload"
	"github.com/sidchai/compkg/pkg/util"
	"io"
	"os"
	"strings"
	"time"
)

type AliyunOss struct {
	ETag            string
	FileSize        int64
	Catalogue       string
	IsTime          bool
	ossBucket       *oss.Bucket
	bucketName      string
	endpoint        string
	IsCustomStorage bool
}

func init() {
	upload.RegisterOss("aliyun-oss", &AliyunOss{})
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
	if err := a.ossBucket.PutObjectFromFile(ossPath, fileLocalPath); err != nil {
		logger.Errorf("AliyunOss UploadFileLocal PutObjectFromFile err:%+v", err.Error())
		return "", err
	}
	fileMd5, fileSize, err := util.CalculateMD5(fileLocalPath)
	if err == nil {
		a.ETag = fileMd5
		a.FileSize = fileSize
	}
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
	client, err := oss.New(endpoint, accessKeyId, accessKeySecret)
	if err != nil {
		hlog.Errorf("AliyunOss NewBucket err:%+v", err.Error())
		return nil, err
	}
	logger.Infof("bucketName:%s", bucketName)
	// 判断桶是否存在，不存在则创建
	//exist, err := client.IsBucketExist(bucketName)
	//if !exist {
	//	// 创建存储空间，并设置存储类型为低频访问oss.StorageIA、读写权限ACL为公共读
	//	if err = client.CreateBucket(bucketName, oss.StorageClass(oss.StorageIA), oss.ACL(oss.ACLPublicRead)); err != nil {
	//		logger.Errorf("AliyunOss CreateBucket err:%+v", err.Error())
	//		return nil, err
	//	}
	//}
	bucket, err := client.Bucket(bucketName)
	if err != nil {
		logger.Errorf("AliyunOss get Bucket err:%+v", err.Error())
		return nil, err
	}
	return bucket, nil
}
