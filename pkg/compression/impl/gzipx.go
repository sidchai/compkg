package impl

import (
	"bytes"
	"compress/gzip"
	"github.com/sidchai/compkg/pkg/compression"
)

type GzipX struct {
}

func init() {
	compression.RegisterCompression("gzip", &GzipX{})
}

func (g *GzipX) Compress(data []byte) ([]byte, error) {
	// 创建一个新的 byte 输出流
	var buf bytes.Buffer
	// 创建一个新的 gzip 输出流
	gzipWriter := gzip.NewWriter(&buf)
	// 将 input byte 数组写入到此输出流中
	_, err := gzipWriter.Write(data)
	if err != nil {
		_ = gzipWriter.Close()
		return nil, err
	}
	if err = gzipWriter.Close(); err != nil {
		return nil, err
	}
	// 返回压缩后的 bytes 数组
	return buf.Bytes(), nil
}

func (g *GzipX) Decompress(data []byte) ([]byte, error) {
	// 创建一个新的 gzip.Reader
	bytesReader := bytes.NewReader(data)
	gzipReader, err := gzip.NewReader(bytesReader)
	if err != nil {
		return nil, err
	}
	defer func() {
		// defer 中关闭 gzipReader
		_ = gzipReader.Close()
	}()
	buf := new(bytes.Buffer)
	// 从 Reader 中读取出数据
	if _, err = buf.ReadFrom(gzipReader); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
