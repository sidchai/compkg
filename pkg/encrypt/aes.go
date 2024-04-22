/**
 * @Description: aes加密
 * @Version: 2.0
 * @Author: sidchai
 * @Date: 2022/8/24
 */

package encrypt

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"errors"
)

const (
	BlockSize = 16
	CBC       = "CBC"
	ECB       = "ECB"
)

var unPaddingFuncMap = map[AesType]UnPaddingFunc{
	AesTypeCBCZeroPadding:  zeroUnPadding,
	AesTypeCBCPKCS7Padding: pKCS7UnPadding,
}

var paddingFuncMap = map[AesType]PaddingFunc{
	AesTypeCBCPKCS7Padding: pKCS7Padding,
	AesTypeCBCZeroPadding:  zeroPadding,
}

const (
	AesTypeCBCZeroPadding AesType = iota
	AesTypeCBCPKCS7Padding
)

type PaddingFunc func([]byte, int) []byte
type UnPaddingFunc func([]byte) []byte
type AesType int

type AesCbc struct {
	AesType AesType `json:"aes_type"`
	Key     string  `json:"key"`
	Iv      string  `json:"iv"`
}

type AesEcb struct {
	AesType AesType `json:"aes_type"`
	Key     string  `json:"key"`
}

func NewAesCbc(aesType AesType, key, iv string) *AesCbc {
	return &AesCbc{
		AesType: aesType,
		Key:     key,
		Iv:      iv,
	}
}

func NewAesEcb(aesType AesType, key string) *AesEcb {
	return &AesEcb{
		AesType: aesType,
		Key:     key,
	}
}

// AesCbcDecrypt
//
//	@Description: aes-cbc解密
//	@param ciphertext 需要解密的文本
//	@return []byte 解密后的数据
//	@return error
func (a *AesCbc) AesCbcDecrypt(ciphertext string) ([]byte, error) {
	unPaddingFunc, ok := unPaddingFuncMap[a.AesType]
	if !ok {
		return nil, errors.New("unsupported Aes type")
	}
	return decrypt(unPaddingFunc, a.Key, a.Iv, CBC, ciphertext)
}

// AesCbcEncrypt
//
//	@Description: aes-cbc加密
//	@param aesType 填充类型
//	@param key 加密密钥
//	@param srcData 需要加密的文本数据
//	@return string 加密后的数据
//	@return error
func (a *AesCbc) AesCbcEncrypt(srcData []byte) (string, error) {
	paddingFn, ok := paddingFuncMap[a.AesType]
	if !ok {
		return "", errors.New("unsupported aes type")
	}
	return encrypt(paddingFn, a.Key, a.Iv, CBC, srcData)
}

func (a *AesEcb) AesEcbDecrypt(ciphertext string) ([]byte, error) {
	unPaddingFunc, ok := unPaddingFuncMap[a.AesType]
	if !ok {
		return nil, errors.New("unsupported Aes type")
	}
	return decrypt(unPaddingFunc, a.Key, "", ECB, ciphertext)
}

// AesEcbEncrypt
//
//	@Description: aes-ecb加密
//	@param aesType 填充类型
//	@param key 加密密钥
//	@param srcData 需要加密的文本数据
//	@return string 加密后的数据
//	@return error
func (a *AesEcb) AesEcbEncrypt(srcData []byte) (string, error) {
	paddingFn, ok := paddingFuncMap[a.AesType]
	if !ok {
		return "", errors.New("unsupported aes type")
	}
	return encrypt(paddingFn, a.Key, "", ECB, srcData)
}

func decrypt(unPaddingFn UnPaddingFunc, key, iv, mode string, ciphertext string) ([]byte, error) {
	ckey, err := aes.NewCipher([]byte(key))
	if nil != err {
		return nil, err
	}
	var decrypter cipher.BlockMode
	switch mode {
	case CBC:
		decrypter = cipher.NewCBCDecrypter(ckey, []byte(iv))
	case ECB:
		decrypter = newECBDecrypter(ckey)
	}
	base64In, _ := base64.StdEncoding.DecodeString(ciphertext)
	in := make([]byte, len(base64In))
	decrypter.CryptBlocks(in, base64In)
	return unPaddingFn(in), nil
}

func encrypt(paddingFn PaddingFunc, key, iv, mode string, srcData []byte) (string, error) {
	ckey, err := aes.NewCipher([]byte(key))
	if nil != err {
		return "", err
	}
	var encrypter cipher.BlockMode
	switch mode {
	case CBC:
		encrypter = cipher.NewCBCEncrypter(ckey, []byte(iv))
	case ECB:
		encrypter = newECBEncrypter(ckey)
	}

	srcData = paddingFn(srcData, BlockSize)
	out := make([]byte, len(srcData))
	encrypter.CryptBlocks(out, srcData)
	return base64.StdEncoding.EncodeToString(out), nil
}

/**
 * PKCS7补码
 */
func pKCS7Padding(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	padtext := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(data, padtext...)
}

func pKCS7UnPadding(data []byte) []byte {
	length := len(data)
	// 去掉最后一个字节 unpadding 次
	unpadding := int(data[length-1])
	return data[:(length - unpadding)]
}

func zeroPadding(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	padtext := bytes.Repeat([]byte{byte(0)}, padding)
	return append(data, padtext...)
}

func zeroUnPadding(origData []byte) []byte {
	length := len(origData)
	i := 0
	for ; 0 == int(origData[length-1-i]); i++ {
	}
	return origData[:(length - i)]
}

type ecb struct {
	b         cipher.Block
	blockSize int
}

func newECB(b cipher.Block) *ecb {
	return &ecb{
		b:         b,
		blockSize: b.BlockSize(),
	}
}

type ecbEncrypter ecb

func newECBEncrypter(b cipher.Block) cipher.BlockMode {
	return (*ecbEncrypter)(newECB(b))
}

func (x *ecbEncrypter) BlockSize() int { return x.blockSize }
func (x *ecbEncrypter) CryptBlocks(dst, src []byte) {
	if len(src)%x.blockSize != 0 {
		panic("crypto/cipher: input not full blocks")
	}
	if len(dst) < len(src) {
		panic("crypto/cipher: output smaller than input")
	}
	for len(src) > 0 {
		x.b.Encrypt(dst, src[:x.blockSize])
		src = src[x.blockSize:]
		dst = dst[x.blockSize:]
	}
}

type ecbDecrypter ecb

func newECBDecrypter(b cipher.Block) cipher.BlockMode {
	return (*ecbDecrypter)(newECB(b))
}
func (x *ecbDecrypter) BlockSize() int { return x.blockSize }
func (x *ecbDecrypter) CryptBlocks(dst, src []byte) {
	if len(src)%x.blockSize != 0 {
		panic("crypto/cipher: input not full blocks")
	}
	if len(dst) < len(src) {
		panic("crypto/cipher: output smaller than input")
	}
	for len(src) > 0 {
		x.b.Decrypt(dst, src[:x.blockSize])
		src = src[x.blockSize:]
		dst = dst[x.blockSize:]
	}
}
