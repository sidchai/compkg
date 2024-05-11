package impl

import (
	"encoding/json"
	"errors"
	"github.com/sidchai/compkg/pkg/compression"
	"github.com/sidchai/compkg/pkg/serialization"
)

type JsonX struct {
}

func init() {
	serialization.RegisterSerialization("json", &JsonX{})
}

func (j *JsonX) Marshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

func (j *JsonX) Unmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

func (j *JsonX) MarshalAndCompression(v interface{}) ([]byte, error) {
	marshal, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	getCompression := compression.GetCompression("gzip")
	if getCompression == nil {
		return nil, errors.New("JsonX no compression found")
	}

	return getCompression.Compress(marshal)
}

func (j *JsonX) UnmarshalAndCompression(data []byte, v interface{}) error {
	getCompression := compression.GetCompression("gzip")
	if getCompression == nil {
		return errors.New("JsonX no compression found")
	}
	data, _ = getCompression.Decompress(data)
	return json.Unmarshal(data, v)
}
