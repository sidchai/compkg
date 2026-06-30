package impl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/sidchai/compkg/pkg/logger"
	"github.com/sidchai/compkg/pkg/upload"
	"github.com/sidchai/compkg/pkg/util"
)

// JDCloudOss 京东云对象存储 OSS 实现
// 京东云 OSS 兼容 AWS S3 协议，依赖 github.com/aws/aws-sdk-go
// endpoint 形如 s3.cn-north-1.jdcloud-oss.com 或 s3.cn-north-1.jcloudcs.com
type JDCloudOss struct {
	ETag            string
	FileSize        int64
	Catalogue       string
	IsTime          bool
	s3Client        *s3.S3
	bucketName      string
	endpoint        string // 不含 scheme，用于拼接对外 URL
	region          string // 京东云 region，如 cn-north-1；AWS S3 SDK 必填
	expires         int64
	IsCustomStorage bool
}

func init() {
	upload.RegisterOss("jdcloud-oss", func() upload.Oss { return &JDCloudOss{} })
}

// NewClient 实例化 S3 客户端，endpoint 不含 scheme（SDK 内部按 DisableSSL=false 走 https）
func (j *JDCloudOss) NewClient(ctx context.Context, opts ...upload.OssOption) {
	copyOpt := upload.DefaultOssOptions
	po := &copyOpt
	for _, opt := range opts {
		opt.Apply(po)
	}
	// 保留无 scheme 的 endpoint 用于拼接外链
	j.endpoint = stripScheme(po.Endpoint)
	// region 优先用 options 传入的；为空时从 endpoint 堆取作为兜底
	j.region = po.Region
	if j.region == "" {
		j.region = parseRegionFromEndpoint(j.endpoint)
	}
	creds := credentials.NewStaticCredentials(po.AccessKeyId, po.SecretAccessKey, "")
	if _, err := creds.Get(); err != nil {
		logger.Errorf("JDCloudOss NewClient credentials err:%+v", err.Error())
		return
	}
	cfg := &aws.Config{
		Region:      aws.String(j.region),
		Endpoint:    aws.String(j.endpoint),
		DisableSSL:  aws.Bool(false), // 默认走 https
		Credentials: creds,
	}
	sess, err := session.NewSession(cfg)
	if err != nil {
		logger.Errorf("JDCloudOss NewClient NewSession err:%+v", err.Error())
		return
	}
	j.s3Client = s3.New(sess)
	j.bucketName = po.BucketName
	j.expires = po.Expires
}

// parseRegionFromEndpoint 从京东云 OSS endpoint 中提取 region，格式为 s3.{region}.jdcloud-oss.com / s3.{region}.jcloudcs.com
// 提取失败返回空串，由 SDK 后续报错提示
func parseRegionFromEndpoint(endpoint string) string {
	parts := strings.Split(endpoint, ".")
	// 期望：[s3, {region}, jdcloud-oss, com] 或 [s3, {region}, jcloudcs, com]
	if len(parts) >= 3 && (parts[0] == "s3" || parts[0] == "s3-internal") {
		return parts[1]
	}
	return ""
}

// UploadFileLocal 上传本地文件到京东云 OSS
func (j *JDCloudOss) UploadFileLocal(fileName, fileLocalPath string) (string, error) {
	if j.s3Client == nil {
		return "", errors.New("s3Client is nil")
	}
	if j.Catalogue == "" {
		j.Catalogue = "audio"
	}
	objectKey := fmt.Sprintf("%s/%s", j.Catalogue, fileName)
	if j.IsTime {
		objectKey = fmt.Sprintf("%s/%s/%s", j.Catalogue, time.Now().Format("2006/01/02"), fileName)
	}
	if j.IsCustomStorage {
		objectKey = strings.ReplaceAll(fileName, fmt.Sprintf("https://%s.%s/", j.bucketName, j.endpoint), "")
	}
	f, err := os.Open(fileLocalPath)
	if err != nil {
		logger.Errorf("JDCloudOss UploadFileLocal os.Open err:%+v", err.Error())
		return "", err
	}
	defer f.Close()
	if _, err := j.s3Client.PutObject(&s3.PutObjectInput{
		Bucket: aws.String(j.bucketName),
		Key:    aws.String(objectKey),
		Body:   f,
	}); err != nil {
		logger.Errorf("JDCloudOss UploadFileLocal PutObject err:%+v", err.Error())
		return "", err
	}
	fileMd5, fileSize, err := util.CalculateMD5(fileLocalPath)
	if err == nil {
		j.ETag = fileMd5
		j.FileSize = fileSize
	}
	return fmt.Sprintf("https://%s.%s/%s", j.bucketName, j.endpoint, objectKey), nil
}

// UploadFileIo 上传文件流到京东云 OSS
// 注意：S3 PutObject 的 Body 需要 io.ReadSeeker，io.Reader 会被 SDK 内部包装；
// 大文件场景建议改用 s3manager.Uploader。
func (j *JDCloudOss) UploadFileIo(fileName string, content io.Reader) (string, error) {
	if j.s3Client == nil {
		return "", errors.New("s3Client is nil")
	}
	if j.Catalogue == "" {
		j.Catalogue = "audio"
	}
	objectKey := fmt.Sprintf("%s/%s", j.Catalogue, fileName)
	if j.IsTime {
		objectKey = fmt.Sprintf("%s/%s/%s", j.Catalogue, time.Now().Format("2006/01/02"), fileName)
	}
	if j.IsCustomStorage {
		objectKey = strings.ReplaceAll(fileName, fmt.Sprintf("https://%s.%s/", j.bucketName, j.endpoint), "")
	}
	// S3 PutObject 要求 Body 实现 io.ReadSeeker，读到内存中再上传
	body, err := io.ReadAll(content)
	if err != nil {
		logger.Errorf("JDCloudOss UploadFileIo ReadAll err:%+v", err.Error())
		return "", err
	}
	output, err := j.s3Client.PutObject(&s3.PutObjectInput{
		Bucket: aws.String(j.bucketName),
		Key:    aws.String(objectKey),
		Body:   bytes.NewReader(body),
	})
	if err != nil {
		logger.Errorf("JDCloudOss UploadFileIo PutObject err:%+v", err.Error())
		return "", err
	}
	if output != nil && output.ETag != nil {
		j.ETag = strings.Trim(*output.ETag, "\"")
	}
	j.FileSize = int64(len(body))
	return fmt.Sprintf("https://%s.%s/%s", j.bucketName, j.endpoint, objectKey), nil
}

// GetETag 获取文件 md5 校验值
func (j *JDCloudOss) GetETag() string {
	return j.ETag
}

// GetFileSize 获取文件大小
func (j *JDCloudOss) GetFileSize() int64 {
	return j.FileSize
}

// SetCatalogue 设置存储目录
func (j *JDCloudOss) SetCatalogue(catalogue string) {
	j.Catalogue = catalogue
}

// SetIsTime 是否在路径中添加日期
func (j *JDCloudOss) SetIsTime(isTime bool) {
	j.IsTime = isTime
}

// SetCustomStorage 是否使用自定义存储路径（URL 直传）
func (j *JDCloudOss) SetCustomStorage(isCustomStorage bool) {
	j.IsCustomStorage = isCustomStorage
}

// Download 从京东云 OSS 下载对象到本地目录
func (j *JDCloudOss) Download(fileUrl, fileName, dataFolder string) error {
	if j.s3Client == nil {
		return errors.New("s3Client is nil")
	}
	objectKey := strings.ReplaceAll(fileUrl, fmt.Sprintf("https://%s.%s/", j.bucketName, j.endpoint), "")
	if !util.CheckFolder(dataFolder) {
		if err := os.MkdirAll(dataFolder, os.ModePerm); err != nil {
			logger.Info("创建目录异常 -> %v", err)
		} else {
			logger.Info("创建成功!")
		}
	}
	file := fmt.Sprintf("%s%s", dataFolder, fileName)
	output, err := j.s3Client.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(j.bucketName),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		logger.Error("JDCloudOss Download GetObject err:%v", err.Error())
		return err
	}
	defer output.Body.Close()
	dst, err := os.Create(file)
	if err != nil {
		logger.Error("JDCloudOss Download os.Create err:%v", err.Error())
		return err
	}
	defer dst.Close()
	if _, err := io.Copy(dst, output.Body); err != nil {
		logger.Error("JDCloudOss Download io.Copy err:%v", err.Error())
		return err
	}
	return nil
}

// GetPresignedURL 生成对象的预签名 GET 链接
func (j *JDCloudOss) GetPresignedURL(path string) (string, error) {
	if j.s3Client == nil {
		return "", errors.New("s3Client is nil")
	}
	objectKey := strings.ReplaceAll(path, fmt.Sprintf("https://%s.%s/", j.bucketName, j.endpoint), "")
	expires := j.expires
	if expires <= 0 {
		expires = 3600
	}
	req, _ := j.s3Client.GetObjectRequest(&s3.GetObjectInput{
		Bucket: aws.String(j.bucketName),
		Key:    aws.String(objectKey),
	})
	signedURL, err := req.Presign(time.Duration(expires) * time.Second)
	if err != nil {
		logger.Errorf("JDCloudOss GetPresignedURL Presign err:%+v", err.Error())
		return "", err
	}
	return signedURL, nil
}

// PutACL 京东云 OSS 暂未实现 ACL 设置（保留接口兼容）
func (j *JDCloudOss) PutACL(path string) error {
	return nil
}

// SetTagging 京东云 OSS 暂未实现对象标签设置（保留接口兼容）
func (j *JDCloudOss) SetTagging(path string, tags map[string]string) error {
	return nil
}

// CopySelf 京东云 OSS 暂未实现深度归档复制（保留接口兼容）
func (j *JDCloudOss) CopySelf(path string, storageClass string) error {
	return nil
}

// Delete 删除京东云 OSS 对象
// path 为完整外链 URL，需先剥离 https://{bucket}.{endpoint}/ 前缀得到 objectKey
func (j *JDCloudOss) Delete(path string) error {
	if j.s3Client == nil {
		return errors.New("s3Client is nil")
	}
	objectKey := strings.ReplaceAll(path, fmt.Sprintf("https://%s.%s/", j.bucketName, j.endpoint), "")
	if _, err := j.s3Client.DeleteObject(&s3.DeleteObjectInput{
		Bucket: aws.String(j.bucketName),
		Key:    aws.String(objectKey),
	}); err != nil {
		logger.Errorf("JDCloudOss Delete DeleteObject err:%+v", err.Error())
		return err
	}
	return nil
}
