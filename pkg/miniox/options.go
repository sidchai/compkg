package miniox

type minioOptions struct {
	BucketName      string // 桶名
	Endpoint        string // 端点
	AccessKeyId     string // accessKey
	SecretAccessKey string // secretKey
	Module          string // 上传目录
	Expires         int64  // 过期时间，单位：s
}

var defaultS3Options = minioOptions{}

type MinioOption interface {
	apply(*minioOptions)
}

type funcMinioOption struct {
	f func(options *minioOptions)
}

func (fpo *funcMinioOption) apply(po *minioOptions) {
	fpo.f(po)
}

func newFuncProducerOption(f func(options *minioOptions)) *funcMinioOption {
	return &funcMinioOption{
		f: f,
	}
}

func WithBucketName(bucketName string) MinioOption {
	return newFuncProducerOption(func(options *minioOptions) {
		options.BucketName = bucketName
	})
}

func WithEndpoint(endpoint string) MinioOption {
	return newFuncProducerOption(func(options *minioOptions) {
		options.Endpoint = endpoint
	})
}

func WithAccessKeyId(accessKeyId string) MinioOption {
	return newFuncProducerOption(func(options *minioOptions) {
		options.AccessKeyId = accessKeyId
	})
}

func WithSecretAccessKey(secretAccessKey string) MinioOption {
	return newFuncProducerOption(func(options *minioOptions) {
		options.SecretAccessKey = secretAccessKey
	})
}

func WithModule(module string) MinioOption {
	return newFuncProducerOption(func(options *minioOptions) {
		options.Module = module
	})
}

func WithExpires(expires int64) MinioOption {
	return newFuncProducerOption(func(options *minioOptions) {
		options.Expires = expires
	})
}
