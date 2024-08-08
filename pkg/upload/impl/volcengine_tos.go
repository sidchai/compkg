package upload

import (
	"context"
	"errors"
	"fmt"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/sidchai/compkg/pkg/logger"
	"github.com/sidchai/compkg/pkg/upload"
	"github.com/volcengine/ve-tos-golang-sdk/v2/tos"
	"io"
	"os"
)

type VolcEngineTos struct {
	ETag       string
	FileSize   int64
	Catalogue  string
	IsTime     bool
	bucketName string
	objectKey  string
	tosClient  *tos.ClientV2
}

func init() {
	upload.RegisterOss("volcengine-tos", &VolcEngineTos{})
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
}

func (v *VolcEngineTos) UploadFileLocal(fileName, fileLocalPath string) (string, error) {
	tosPath := fmt.Sprintf("%s/%s/%s", v.objectKey, v.Catalogue, fileName)
	if v.tosClient == nil {
		return "", errors.New("tosClient is nil")
	}
	f, err := os.Open(fileLocalPath)
	if err != nil {
		logger.Errorf("VolcEnginTos UploadFileLocal os.Open err:%+v", err.Error())
		return "", err
	}
	defer f.Close()
	fileInfo, _ := f.Stat()
	output, err := v.tosClient.PutObjectV2(context.Background(), &tos.PutObjectV2Input{
		PutObjectBasicInput: tos.PutObjectBasicInput{
			Bucket: v.bucketName,
			Key:    tosPath,
		},
		Content: f,
	})
	v.ETag = output.ETag
	v.FileSize = fileInfo.Size()
	return fmt.Sprintf("voldtos://%s/%s", v.bucketName, tosPath), nil
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

	return fmt.Sprintf("voldtos://%s/%s", v.bucketName, tosPath), nil
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

func (v *VolcEngineTos) Download(fileUrl, fileName, dataFolder string) error {
	return nil
}

func (v *VolcEngineTos) PutACL(path string) error {
	return nil
}

func (v *VolcEngineTos) SetTagging(path string, tags map[string]string) error {
	return nil
}

func NewClientV2(endpoint, accessKey, secretKey, region string) (*tos.ClientV2, error) {
	tosClient, err := tos.NewClientV2(endpoint, tos.WithRegion(region), tos.WithCredentials(tos.NewStaticCredentials(accessKey, secretKey)))
	if err != nil {
		hlog.Errorf("VolcEngineTos NewClientV2 err:%+v", err.Error())
		return nil, err
	}
	return tosClient, nil
}
