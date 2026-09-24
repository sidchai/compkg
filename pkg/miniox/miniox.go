package miniox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/sidchai/compkg/pkg/logger"
)

var (
	minioClient *minio.Client
)

type MinioX struct {
	ctx        context.Context
	BucketName string        // 桶名
	Module     string        // 目录名
	ObjectSize int64         // 文件大小
	Expires    int64         // 访问链接过期时间
	ETag       string        // 文件md5校验值
	client     *minio.Client // 操作客户端
}

func initMinioX(endpoint, accessKeyId, secretAccessKey string, secure bool) *minio.Client {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKeyId, secretAccessKey, ""),
		Secure: secure,
	})
	if err != nil {
		fmt.Println("miniox initMinioX err:", err)
		return nil
	}
	//minioClient = client
	return client
}

func NewMinioX(ctx context.Context, opts ...MinioOption) *MinioX {
	copyOpt := defaultS3Options
	po := &copyOpt
	for _, opt := range opts {
		opt.apply(po)
	}
	minioX := &MinioX{
		ctx:        ctx,
		BucketName: po.BucketName,
		Module:     po.Module,
		Expires:    po.Expires,
		client:     minioClient,
	}
	//if minioClient == nil {
	//	initMinioX(po.Endpoint, po.AccessKeyId, po.SecretAccessKey, po.Secure)
	//	minioX.client = minioClient
	//}
	minioX.client = initMinioX(po.Endpoint, po.AccessKeyId, po.SecretAccessKey, po.Secure)
	//policy := fmt.Sprintf(`{"Version": "2012-10-17","Statement": [{"Action": ["s3:GetObject"],"Effect": "Allow","Principal": {"AWS": ["*"]},"Resource": ["arn:aws:s3:::%s/*"],"Sid": ""}]}`, po.BucketName)
	//err := minioClient.SetBucketPolicy(ctx, po.BucketName, policy)
	//if err != nil {
	//	log.Println("miniox SetBucketPolicy err:", err)
	//}
	//minioX.Bucket()
	return minioX
}

func (m *MinioX) Bucket() {
	if exists, _ := m.client.BucketExists(m.ctx, m.BucketName); !exists {
		// 创建bucket
		m.client.MakeBucket(m.ctx, m.BucketName, minio.MakeBucketOptions{})
	}
}

// UploadFile
//
//	@Description: 上传文件
//	@param filePath 待上传文件路径
//	@return error
func (m *MinioX) UploadFile(objectName, filePath string) (string, error) {
	if m.client == nil {
		return "", errors.New("miniox client is nil")
	}
	options := minio.PutObjectOptions{}
	uploadInfo, err := m.client.FPutObject(m.ctx, m.BucketName, m.Module+objectName, filePath, options)
	if err != nil {
		logger.Errorf("miniox uploadFile err:%v", err)
		return "", err
	}
	logger.Infof("miniox uploadFile success, key:%s etag:%s, size:%d", uploadInfo.Key, uploadInfo.ETag, uploadInfo.Size)
	// 文件大小
	m.ObjectSize = uploadInfo.Size
	// 文件md5校验值
	m.ETag = uploadInfo.ETag
	return fmt.Sprintf("%s://%s/%s/%s", m.client.EndpointURL().Scheme, m.client.EndpointURL().Host, m.BucketName, m.Module+objectName), nil
}

// Upload
//
//	@Description: 上传文件
//	@param reader 文件流数据
//	@param objectSize 文件大小
//	@return error
func (m *MinioX) Upload(objectName string, reader io.Reader, objectSize int64) (string, error) {
	if m.client == nil {
		return "", errors.New("miniox client is nil")
	}
	uploadInfo, err := m.client.PutObject(m.ctx, m.BucketName, m.Module+objectName, reader, objectSize, minio.PutObjectOptions{})
	if err != nil {
		logger.Errorf("miniox upload err:%v", err)
		return "", err
	}
	// 文件大小
	m.ObjectSize = uploadInfo.Size
	// 文件md5校验值
	m.ETag = uploadInfo.ETag
	return fmt.Sprintf("%s://%s/%s%s", m.client.EndpointURL().Scheme, m.client.EndpointURL().Host, m.BucketName, m.Module+objectName), nil
}

// DownloadFile
//
//	@Description: 下载文件到本地
//	@param localFileName
//	@return error
func (m *MinioX) DownloadFile(fileUrl, localFileName string) error {
	objectName := strings.ReplaceAll(fileUrl, fmt.Sprintf("%s://%s/%s/", m.client.EndpointURL().Scheme, m.client.EndpointURL().Host, m.BucketName), "")
	logger.Info(objectName)
	return m.client.FGetObject(m.ctx, m.BucketName, objectName, localFileName, minio.GetObjectOptions{})
}

// GetObject
//
//	@Description: 获取目标文件信息
//	@return error
func (m *MinioX) GetObject(fileUrl string) *minio.Object {
	objectName := strings.ReplaceAll(fileUrl, fmt.Sprintf("%s://%s", m.client.EndpointURL().Scheme, m.client.EndpointURL().Host), "")
	object, err := m.client.GetObject(m.ctx, m.BucketName, objectName, minio.GetObjectOptions{})
	if err != nil {
		logger.Errorf("miniox download err:%v", err)
		return nil
	}
	return object
}

// GetObjectUrl 按「目录 Module + 文件名」生成预签名 GET 链接。
// 必须返回 u.String()：签名在 query（X-Amz-*），只拼 Path 会丢掉凭证。
func (m *MinioX) GetObjectUrl(objectName string) (objectUrl string, err error) {
	if m.client == nil {
		return "", errors.New("miniox client is nil")
	}
	u, err := m.client.PresignedGetObject(m.ctx, m.BucketName, m.Module+objectName, m.presignExpires(), nil)
	if err != nil {
		logger.Errorf("miniox GetObjectUrl err:%v", err)
		return "", err
	}
	return u.String(), nil
}

// DeleteObject
//
//	@Description: 根据完整外链 URL 删除对象
//	@param fileUrl 完整外链（包含 scheme/host/bucket）
//	@return error
func (m *MinioX) DeleteObject(fileUrl string) error {
	if m.client == nil {
		return errors.New("miniox client is nil")
	}
	objectName := strings.ReplaceAll(fileUrl, fmt.Sprintf("%s://%s/%s/", m.client.EndpointURL().Scheme, m.client.EndpointURL().Host, m.BucketName), "")
	return m.client.RemoveObject(m.ctx, m.BucketName, objectName, minio.RemoveObjectOptions{})
}

// GetPresignedURL 根据完整外链或裸 object key 生成预签名 GET 链接。
// 不能走 GetObjectUrl：后者会再拼 Module，完整 URL 剥出的 key 已含目录。
func (m *MinioX) GetPresignedURL(fileUrl string) (objectUrl string, err error) {
	if m.client == nil {
		return "", errors.New("miniox client is nil")
	}
	objectName := m.objectNameFromURL(fileUrl)
	if objectName == "" {
		return "", errors.New("object key is empty")
	}
	u, err := m.client.PresignedGetObject(m.ctx, m.BucketName, objectName, m.presignExpires(), nil)
	if err != nil {
		logger.Errorf("miniox GetPresignedURL err:%v", err)
		return "", err
	}
	return u.String(), nil
}

// presignExpires minio-go 要求 expires > 0，未配置时与其它 OSS 实现一致兜底 3600s。
func (m *MinioX) presignExpires() time.Duration {
	expires := m.Expires
	if expires <= 0 {
		expires = 3600
	}
	return time.Duration(expires) * time.Second
}

// objectNameFromURL 从完整外链或裸 key 得到真实 object key（UTF-8，已 PathUnescape）。
// 兼容 path-style（/{bucket}/{key}）与虚拟主机风格（/{key}），并丢掉已有签名 query。
func (m *MinioX) objectNameFromURL(fileUrl string) string {
	fileUrl = strings.TrimSpace(fileUrl)
	if fileUrl == "" {
		return ""
	}
	if q := strings.Index(fileUrl, "?"); q >= 0 {
		fileUrl = fileUrl[:q]
	}
	if !strings.Contains(fileUrl, "://") {
		return unescapeMinioObjectKey(strings.TrimPrefix(fileUrl, "/"))
	}
	u, err := url.Parse(fileUrl)
	if err != nil {
		return unescapeMinioObjectKey(fileUrl)
	}
	path := u.EscapedPath()
	if path == "" {
		path = u.Path
	}
	path = strings.TrimPrefix(path, "/")
	if m.BucketName != "" && strings.HasPrefix(path, m.BucketName+"/") {
		path = strings.TrimPrefix(path, m.BucketName+"/")
	}
	return unescapeMinioObjectKey(path)
}

// unescapeMinioObjectKey 将 URL 编码的 object key 还原为上传时的真实 key。
// 最多解两轮，避免对已编码中文 key 再签一次变成 %25xx。
func unescapeMinioObjectKey(key string) string {
	key = strings.TrimPrefix(key, "/")
	if key == "" {
		return key
	}
	for i := 0; i < 2; i++ {
		if !strings.Contains(key, "%") {
			break
		}
		decoded, err := url.PathUnescape(key)
		if err != nil || decoded == key {
			break
		}
		key = decoded
	}
	return key
}
