package impl

import (
	"context"
	"fmt"
	"github.com/sidchai/compkg/pkg/logger"
	"github.com/sidchai/compkg/pkg/upload"
	"github.com/sidchai/compkg/pkg/util"
	"github.com/tencentyun/cos-go-sdk-v5"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type TencentCos struct {
	ETag            string
	FileSize        int64
	Catalogue       string
	IsTime          bool
	cosClient       *cos.Client
	cosUrl          string
	IsCustomStorage bool
}

func init() {
	upload.RegisterOss("tencent-cos", &TencentCos{})
}

func (t *TencentCos) NewClient(ctx context.Context, opts ...upload.OssOption) {
	copyOpt := upload.DefaultOssOptions
	po := &copyOpt
	for _, opt := range opts {
		opt.Apply(po)
	}
	t.cosUrl = po.Endpoint
	t.cosClient = NewClient(po.Endpoint, po.AccessKeyId, po.SecretAccessKey)
}

func (t *TencentCos) UploadFileLocal(fileName, fileLocalPath string) (string, error) {
	if t.Catalogue == "" {
		t.Catalogue = "audio"
	}
	cosPath := fmt.Sprintf("/%s/%s", t.Catalogue, fileName)
	if t.IsTime {
		cosPath = fmt.Sprintf("/%s/%s/%s", t.Catalogue, time.Now().Format("2006/01/02"), fileName)
	}
	if t.IsCustomStorage {
		cosPath = strings.ReplaceAll(fileName, t.cosUrl+"/", "")
	}
	headerOptions := &cos.ObjectPutHeaderOptions{
		XCosStorageClass: "STANDARD_IA",
	}
	if strings.Contains(fileName, "TXT") || strings.Contains(fileName, "txt") {
		headerOptions.ContentType = "text/plain; charset=utf-8"
	}
	opt := &cos.ObjectPutOptions{
		ObjectPutHeaderOptions: headerOptions,
		ACLHeaderOptions: &cos.ACLHeaderOptions{
			XCosACL: "public-read",
		},
	}
	rsp, err := t.cosClient.Object.PutFromFile(context.Background(), cosPath, fileLocalPath, opt)
	if err != nil {
		logger.Errorf("TencentCos UploadFileLocal PutFromFile err:%+v", err.Error())
		return "", err
	}
	// 获取文件md5校验值
	etag := rsp.Header.Get("ETag")
	t.ETag = util.RemoveQuotes(etag)
	t.FileSize = rsp.Request.ContentLength
	return fmt.Sprintf("%s%s", t.cosUrl, cosPath), nil
}

func (t *TencentCos) UploadFileIo(fileName string, content io.Reader) (string, error) {
	cosPath := fmt.Sprintf("/%s/%s", t.Catalogue, fileName)
	if t.IsTime {
		cosPath = fmt.Sprintf("/%s/%s/%s", t.Catalogue, time.Now().Format("2006/01/02"), fileName)
	}
	if t.IsCustomStorage {
		cosPath = strings.ReplaceAll(fileName, t.cosUrl+"/", "")
	}
	rsp, err := t.cosClient.Object.Put(context.Background(), cosPath, content, nil)
	if err != nil {
		logger.Errorf("TencentCos UploadFileIo Put err:%+v", err.Error())
		return "", err
	}
	// 获取文件md5校验值
	etag := rsp.Header.Get("ETag")
	t.ETag = util.RemoveQuotes(etag)
	t.FileSize = rsp.Request.ContentLength
	return fmt.Sprintf("%s%s", t.cosUrl, cosPath), nil
}

func (t *TencentCos) GetETag() string {
	return t.ETag
}

func (t *TencentCos) GetFileSize() int64 {
	return t.FileSize
}

func (t *TencentCos) SetCatalogue(catalogue string) {
	t.Catalogue = catalogue
}

func (t *TencentCos) SetIsTime(isTime bool) {
	t.IsTime = isTime
}

func (t *TencentCos) SetCustomStorage(isCustomStorage bool) {
	t.IsCustomStorage = isCustomStorage
}

func (t *TencentCos) Download(fileUrl, fileName, dataFolder string) error {
	key := strings.ReplaceAll(fileUrl, t.cosUrl+"/", "")
	if !util.CheckFolder(dataFolder) {
		err := os.MkdirAll(dataFolder, os.ModePerm)
		if err != nil {
			logger.Info("创建目录异常 -> %v", err)
		} else {
			logger.Info("创建成功!")
		}
	}
	file := fmt.Sprintf("%s%s", dataFolder, fileName)
	opt := &cos.MultiDownloadOptions{
		ThreadPoolSize: 5, // 线程池大小
	}
	_, err := t.cosClient.Object.Download(context.Background(), key, file, opt)
	if err != nil {
		logger.Error("TencentCos Download Download err:%v", err.Error())
		return err
	}
	return nil
}

func (t *TencentCos) PutACL(path string) error {
	opt := &cos.ObjectPutACLOptions{
		Header: &cos.ACLHeaderOptions{
			XCosACL: "private",
		},
	}
	path = strings.ReplaceAll(path, t.cosUrl+"/", "")
	_, err := t.cosClient.Object.PutACL(context.Background(), path, opt)
	if err != nil {
		return err
	}
	return nil
}

func (t *TencentCos) SetTagging(path string, tags map[string]string) error {
	tagSet := make([]cos.ObjectTaggingTag, 0)
	for k, v := range tags {
		tagSet = append(tagSet, cos.ObjectTaggingTag{
			Key:   k,
			Value: v,
		})
	}
	path = strings.ReplaceAll(path, t.cosUrl+"/", "")
	// 设置标签
	opt := &cos.ObjectPutTaggingOptions{
		TagSet: tagSet,
	}
	_, err := t.cosClient.Object.PutTagging(context.Background(), path, opt)
	if err != nil {
		return err
	}
	return nil
}

func NewClient(cosUrl, secretId, secretKey string) *cos.Client {
	urlStr, _ := url.Parse(cosUrl)
	baseURL := &cos.BaseURL{BucketURL: urlStr}
	client := cos.NewClient(baseURL, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:  secretId,
			SecretKey: secretKey,
		},
	})
	return client
}
