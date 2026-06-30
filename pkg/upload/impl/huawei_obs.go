package impl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/huaweicloud/huaweicloud-sdk-go-obs/obs"
	"github.com/sidchai/compkg/pkg/logger"
	"github.com/sidchai/compkg/pkg/upload"
	"github.com/sidchai/compkg/pkg/util"
)

// HuaweiObs 华为云 OBS 对象存储实现
type HuaweiObs struct {
	ETag            string
	FileSize        int64
	Catalogue       string
	IsTime          bool
	obsClient       *obs.ObsClient
	bucketName      string
	endpoint        string // 不含 scheme，用于拼接对外 URL，例如 obs.cn-north-4.myhuaweicloud.com
	expires         int64
	IsCustomStorage bool
}

func init() {
	upload.RegisterOss("huawei-obs", func() upload.Oss { return &HuaweiObs{} })
}

// NewClient 实例化 OBS 客户端，endpoint 自动补全 https:// 前缀
func (h *HuaweiObs) NewClient(ctx context.Context, opts ...upload.OssOption) {
	copyOpt := upload.DefaultOssOptions
	po := &copyOpt
	for _, opt := range opts {
		opt.Apply(po)
	}
	// 保留无 scheme 的 endpoint 用于拼接外链
	h.endpoint = stripScheme(po.Endpoint)
	endpointForSDK := po.Endpoint
	if !strings.HasPrefix(endpointForSDK, "http://") && !strings.HasPrefix(endpointForSDK, "https://") {
		endpointForSDK = "https://" + endpointForSDK
	}
	client, err := obs.New(po.AccessKeyId, po.SecretAccessKey, endpointForSDK)
	if err != nil {
		logger.Errorf("HuaweiObs NewClient err:%+v", err.Error())
		return
	}
	h.obsClient = client
	h.bucketName = po.BucketName
	h.expires = po.Expires
}

// UploadFileLocal 上传本地文件到 OBS
func (h *HuaweiObs) UploadFileLocal(fileName, fileLocalPath string) (string, error) {
	if h.obsClient == nil {
		return "", errors.New("obsClient is nil")
	}
	if h.Catalogue == "" {
		h.Catalogue = "audio"
	}
	objectKey := fmt.Sprintf("%s/%s", h.Catalogue, fileName)
	if h.IsTime {
		objectKey = fmt.Sprintf("%s/%s/%s", h.Catalogue, time.Now().Format("2006/01/02"), fileName)
	}
	if h.IsCustomStorage {
		objectKey = strings.ReplaceAll(fileName, fmt.Sprintf("https://%s.%s/", h.bucketName, h.endpoint), "")
	}
	input := &obs.PutFileInput{}
	input.Bucket = h.bucketName
	input.Key = objectKey
	input.SourceFile = fileLocalPath
	if _, err := h.obsClient.PutFile(input); err != nil {
		logger.Errorf("HuaweiObs UploadFileLocal PutFile err:%+v", err.Error())
		return "", err
	}
	fileMd5, fileSize, err := util.CalculateMD5(fileLocalPath)
	if err == nil {
		h.ETag = fileMd5
		h.FileSize = fileSize
	}
	return fmt.Sprintf("https://%s.%s/%s", h.bucketName, h.endpoint, objectKey), nil
}

// UploadFileIo 上传文件流到 OBS
func (h *HuaweiObs) UploadFileIo(fileName string, content io.Reader) (string, error) {
	if h.obsClient == nil {
		return "", errors.New("obsClient is nil")
	}
	if h.Catalogue == "" {
		h.Catalogue = "audio"
	}
	objectKey := fmt.Sprintf("%s/%s", h.Catalogue, fileName)
	if h.IsTime {
		objectKey = fmt.Sprintf("%s/%s/%s", h.Catalogue, time.Now().Format("2006/01/02"), fileName)
	}
	if h.IsCustomStorage {
		objectKey = strings.ReplaceAll(fileName, fmt.Sprintf("https://%s.%s/", h.bucketName, h.endpoint), "")
	}
	input := &obs.PutObjectInput{}
	input.Bucket = h.bucketName
	input.Key = objectKey
	input.Body = content
	output, err := h.obsClient.PutObject(input)
	if err != nil {
		logger.Errorf("HuaweiObs UploadFileIo PutObject err:%+v", err.Error())
		return "", err
	}
	h.ETag = strings.Trim(output.ETag, "\"")
	return fmt.Sprintf("https://%s.%s/%s", h.bucketName, h.endpoint, objectKey), nil
}

// GetETag 获取文件 md5 校验值
func (h *HuaweiObs) GetETag() string {
	return h.ETag
}

// GetFileSize 获取文件大小
func (h *HuaweiObs) GetFileSize() int64 {
	return h.FileSize
}

// SetCatalogue 设置存储目录
func (h *HuaweiObs) SetCatalogue(catalogue string) {
	h.Catalogue = catalogue
}

// SetIsTime 是否在路径中添加日期
func (h *HuaweiObs) SetIsTime(isTime bool) {
	h.IsTime = isTime
}

// SetCustomStorage 是否使用自定义存储路径（URL 直传）
func (h *HuaweiObs) SetCustomStorage(isCustomStorage bool) {
	h.IsCustomStorage = isCustomStorage
}

// Download 下载对象到本地目录
func (h *HuaweiObs) Download(fileUrl, fileName, dataFolder string) error {
	if h.obsClient == nil {
		return errors.New("obsClient is nil")
	}
	objectKey := strings.ReplaceAll(fileUrl, fmt.Sprintf("https://%s.%s/", h.bucketName, h.endpoint), "")
	if !util.CheckFolder(dataFolder) {
		if err := os.MkdirAll(dataFolder, os.ModePerm); err != nil {
			logger.Info("创建目录异常 -> %v", err)
		} else {
			logger.Info("创建成功!")
		}
	}
	file := fmt.Sprintf("%s%s", dataFolder, fileName)
	input := &obs.DownloadFileInput{}
	input.Bucket = h.bucketName
	input.Key = objectKey
	input.DownloadFile = file
	input.EnableCheckpoint = true
	if _, err := h.obsClient.DownloadFile(input); err != nil {
		logger.Error("HuaweiObs Download DownloadFile err:%v", err.Error())
		return err
	}
	return nil
}

// GetPresignedURL 生成对象的预签名下载链接
func (h *HuaweiObs) GetPresignedURL(path string) (string, error) {
	if h.obsClient == nil {
		return "", errors.New("obsClient is nil")
	}
	objectKey := strings.ReplaceAll(path, fmt.Sprintf("https://%s.%s/", h.bucketName, h.endpoint), "")
	expires := h.expires
	if expires <= 0 {
		expires = 3600
	}
	input := &obs.CreateSignedUrlInput{
		Method:  obs.HttpMethodGet,
		Bucket:  h.bucketName,
		Key:     objectKey,
		Expires: int(expires),
	}
	output, err := h.obsClient.CreateSignedUrl(input)
	if err != nil {
		logger.Errorf("HuaweiObs GetPresignedURL CreateSignedUrl err:%+v", err.Error())
		return "", err
	}
	return output.SignedUrl, nil
}

// PutACL 华为云 OBS 暂未实现 ACL 设置（保留接口兼容）
func (h *HuaweiObs) PutACL(path string) error {
	return nil
}

// SetTagging 华为云 OBS 暂未实现对象标签设置（保留接口兼容）
func (h *HuaweiObs) SetTagging(path string, tags map[string]string) error {
	return nil
}

// CopySelf 华为云 OBS 暂未实现深度归档复制（保留接口兼容）
func (h *HuaweiObs) CopySelf(path string, storageClass string) error {
	return nil
}

// Delete 删除华为云 OBS 对象
// path 为完整外链 URL，需先剥离 https://{bucket}.{endpoint}/ 前缀得到 objectKey
func (h *HuaweiObs) Delete(path string) error {
	if h.obsClient == nil {
		return errors.New("obsClient is nil")
	}
	objectKey := strings.ReplaceAll(path, fmt.Sprintf("https://%s.%s/", h.bucketName, h.endpoint), "")
	input := &obs.DeleteObjectInput{}
	input.Bucket = h.bucketName
	input.Key = objectKey
	if _, err := h.obsClient.DeleteObject(input); err != nil {
		logger.Errorf("HuaweiObs Delete DeleteObject err:%+v", err.Error())
		return err
	}
	return nil
}

// stripScheme 移除 endpoint 中的 http(s):// 前缀，用于拼接对外 URL
func stripScheme(endpoint string) string {
	if strings.HasPrefix(endpoint, "https://") {
		return strings.TrimPrefix(endpoint, "https://")
	}
	if strings.HasPrefix(endpoint, "http://") {
		return strings.TrimPrefix(endpoint, "http://")
	}
	return endpoint
}
