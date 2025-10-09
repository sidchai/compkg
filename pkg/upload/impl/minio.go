package impl

import (
	"context"
	"fmt"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/sidchai/compkg/pkg/logger"
	"github.com/sidchai/compkg/pkg/miniox"
	"github.com/sidchai/compkg/pkg/upload"
	"github.com/sidchai/compkg/pkg/util"
	"io"
	"os"
	"time"
)

type Minio struct {
	ETag            string
	FileSize        int64
	Catalogue       string
	IsTime          bool
	client          *miniox.MinioX
	IsCustomStorage bool
}

func (m *Minio) GetPresignedURL(path string) (string, error) {
	return m.client.GetPresignedURL(path)
}

func init() {
	upload.RegisterOss("minio", &Minio{})
	upload.RegisterOss("s3", &Minio{})
}

func (m *Minio) NewClient(ctx context.Context, opts ...upload.OssOption) {
	copyOpt := upload.DefaultOssOptions
	po := &copyOpt
	for _, opt := range opts {
		opt.Apply(po)
	}
	minioX := miniox.NewMinioX(context.Background(),
		miniox.WithBucketName(po.BucketName),
		miniox.WithAccessKeyId(po.AccessKeyId),
		miniox.WithSecretAccessKey(po.SecretAccessKey),
		miniox.WithEndpoint(po.Endpoint),
		miniox.WithSecure(po.Secure),
	)
	m.client = minioX
}

func (m *Minio) UploadFileLocal(fileName, fileLocalPath string) (string, error) {
	if m.client == nil {
		return "", fmt.Errorf("minio client is nil")
	}
	if m.Catalogue == "" {
		m.Catalogue = "audio"
	}
	if m.IsTime {
		m.Catalogue = fmt.Sprintf("%s/%s/", m.Catalogue, time.Now().Format("2006/01/02"))
	}
	m.client.Module = m.Catalogue
	url, err := m.client.UploadFile(fileName, fileLocalPath)
	if err != nil {
		logger.Errorf("Local UploadFileLocal UploadFile err:%+v", err.Error())
		return "", err
	}
	m.ETag = m.client.ETag
	m.FileSize = m.client.ObjectSize
	return url, nil
}

func (m *Minio) UploadFileIo(fileName string, content io.Reader) (string, error) {
	if m.client == nil {
		return "", fmt.Errorf("minio client is nil")
	}
	if m.Catalogue == "" {
		m.Catalogue = "audio"
	}
	if m.IsTime {
		m.Catalogue = fmt.Sprintf("/%s/%s/", m.Catalogue, time.Now().Format("2006/01/02"))
	}
	m.client.Module = m.Catalogue
	readerLen, _ := oss.GetReaderLen(content)
	url, err := m.client.Upload(fileName, content, readerLen)
	if err != nil {
		hlog.Errorf("Local UploadFileIo Upload err:%+v", err.Error())
		return "", err
	}
	m.ETag = m.client.ETag
	m.FileSize = m.client.ObjectSize
	return url, nil
}

func (m *Minio) GetETag() string {
	return m.ETag
}

func (m *Minio) GetFileSize() int64 {
	return m.FileSize
}

func (m *Minio) SetCatalogue(catalogue string) {
	m.Catalogue = catalogue
}

func (m *Minio) SetIsTime(isTime bool) {
	m.IsTime = isTime
}

func (m *Minio) SetCustomStorage(isCustomStorage bool) {
	m.IsCustomStorage = isCustomStorage
}

func (m *Minio) Download(fileUrl, fileName, dataFolder string) error {
	if !util.CheckFolder(dataFolder) {
		err := os.MkdirAll(dataFolder, os.ModePerm)
		if err != nil {
			logger.Info("创建目录异常 -> %v", err)
		} else {
			logger.Info("创建成功!")
		}
	}
	file := fmt.Sprintf("%s%s", dataFolder, fileName)
	err := m.client.DownloadFile(fileUrl, file)
	if err != nil {
		logger.Error("minio Download DownloadFile err:%v", err.Error())
		return err
	}
	return nil
}

func (m *Minio) PutACL(path string) error {

	return nil
}

func (m *Minio) SetTagging(path string, tags map[string]string) error {

	return nil
}
