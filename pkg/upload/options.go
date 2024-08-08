package upload

type OssOptions struct {
	BucketName      string // 桶名
	Endpoint        string // 端点
	Region          string // 地区
	AccessKeyId     string // accessKey
	SecretAccessKey string // secretKey
	Expires         int64  // 过期时间，单位：s
	ObjectKey       string // 对象key
}

var DefaultOssOptions = OssOptions{}

type OssOption interface {
	Apply(*OssOptions)
}

type funcOssOption struct {
	f func(options *OssOptions)
}

func (fpo *funcOssOption) Apply(po *OssOptions) {
	fpo.f(po)
}

func newFuncProducerOption(f func(options *OssOptions)) *funcOssOption {
	return &funcOssOption{
		f: f,
	}
}

func WithBucketName(bucketName string) OssOption {
	return newFuncProducerOption(func(options *OssOptions) {
		options.BucketName = bucketName
	})
}

func WithEndpoint(endpoint string) OssOption {
	return newFuncProducerOption(func(options *OssOptions) {
		options.Endpoint = endpoint
	})
}

func WithRegion(region string) OssOption {
	return newFuncProducerOption(func(options *OssOptions) {
		options.Region = region
	})
}

func WithAccessKeyId(accessKeyId string) OssOption {
	return newFuncProducerOption(func(options *OssOptions) {
		options.AccessKeyId = accessKeyId
	})
}

func WithSecretAccessKey(secretAccessKey string) OssOption {
	return newFuncProducerOption(func(options *OssOptions) {
		options.SecretAccessKey = secretAccessKey
	})
}

func WithExpires(expires int64) OssOption {
	return newFuncProducerOption(func(options *OssOptions) {
		options.Expires = expires
	})
}

func WithObjectKey(objectKey string) OssOption {
	return newFuncProducerOption(func(options *OssOptions) {
		options.ObjectKey = objectKey
	})
}
