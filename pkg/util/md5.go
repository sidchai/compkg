package util

import (
	"bufio"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"time"
)

const bufferSize = 4096 // 缓冲区大小

func CalculateMD5(filePath string) (string, int64, error) {
	begin := time.Now()
	file, err := os.Open(filePath)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()

	fileInfo, _ := file.Stat()

	// 创建MD5哈希对象
	hash := md5.New()

	// 创建带有缓冲区的读取器
	reader := bufio.NewReaderSize(file, bufferSize)

	// 缓冲读取并写入哈希对象
	buffer := make([]byte, bufferSize)
	for {
		n, err := reader.Read(buffer)
		if err != nil && err != io.EOF {
			fmt.Println("读取文件失败:", err)
			return "", 0, err
		}

		if n == 0 {
			break
		}

		hash.Write(buffer[:n])
	}

	// 计算MD5哈希值
	md5Hash := hash.Sum(nil)

	// 将MD5哈希值转换为十六进制字符串
	md5String := hex.EncodeToString(md5Hash)

	elapsed := time.Since(begin)
	fmt.Println("耗时:", fmt.Sprintf("%0.3fms", float64(elapsed.Nanoseconds())/1e6))

	return md5String, fileInfo.Size(), nil
}
