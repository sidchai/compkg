package impl

import (
	"errors"
	"github.com/bytedance/sonic"
	"github.com/sidchai/compkg/pkg/compression"
	"github.com/sidchai/compkg/pkg/serialization"
)

type SonicX struct {
}

func init() {
	serialization.RegisterSerialization("sonic", &SonicX{})
}
func (s *SonicX) Marshal(v interface{}) ([]byte, error) {
	return sonic.Marshal(v)
}

func (s *SonicX) Unmarshal(data []byte, v interface{}) error {
	return sonic.Unmarshal(data, v)
}

func (s *SonicX) MarshalAndCompression(v interface{}) ([]byte, error) {
	marshal, err := sonic.Marshal(v)
	if err != nil {
		return nil, err
	}
	getCompression := compression.GetCompression("gzip")
	if getCompression == nil {
		return nil, errors.New("SonicX no compression found")
	}

	return getCompression.Compress(marshal)
}

func (s *SonicX) UnmarshalAndCompression(data []byte, v interface{}) error {
	getCompression := compression.GetCompression("gzip")
	if getCompression == nil {
		return errors.New("SonicX no compression found")
	}
	data, _ = getCompression.Decompress(data)
	return sonic.Unmarshal(data, v)
}
