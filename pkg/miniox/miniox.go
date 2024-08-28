package miniox

import (
	"context"
	"fmt"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/sidchai/compkg/pkg/logger"
	"io"
	"os"
	"strings"
	"time"
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

func initMinioX(endpoint, accessKeyId, secretAccessKey string) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKeyId, secretAccessKey, ""),
		Secure: true,
	})
	if err != nil {
		fmt.Println("miniox initMinioX err:", err)
		os.Exit(-1)
		return
	}
	minioClient = client
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
	if minioClient == nil {
		initMinioX(po.Endpoint, po.AccessKeyId, po.SecretAccessKey)
		minioX.client = minioClient
	}
	//policy := fmt.Sprintf(`{"Version": "2012-10-17","Statement": [{"Action": ["s3:GetObject"],"Effect": "Allow","Principal": {"AWS": ["*"]},"Resource": ["arn:aws:s3:::%s/*"],"Sid": ""}]}`, po.BucketName)
	//err := minioClient.SetBucketPolicy(ctx, po.BucketName, policy)
	//if err != nil {
	//	log.Println("miniox SetBucketPolicy err:", err)
	//}
	minioX.Bucket()
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
	options := minio.PutObjectOptions{}
	uploadInfo, err := m.client.FPutObject(m.ctx, m.BucketName, m.Module+objectName, filePath, options)
	if err != nil {
		logger.Errorf("miniox uploadFile err:%v", err)
		return "", err
	}
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

// GetObjectUrl
//
//	@Description: 获取文件临时访问路径
//	@return err
//	@return objectUrl
func (m *MinioX) GetObjectUrl(objectName string) (err error, objectUrl string) {
	u, err := m.client.PresignedGetObject(m.ctx, m.BucketName, m.Module+objectName, time.Duration(m.Expires)*time.Second, nil)
	if err != nil {
		logger.Errorf("miniox GetObjectUrl err:%v", err)
		return err, ""
	}
	objectUrl = fmt.Sprintf("%s://%s%s", u.Scheme, u.Host, u.Path)
	return nil, objectUrl
}
