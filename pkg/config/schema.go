package config

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// compileSchema 编译 JSON Schema（draft-07/2020-12 自适应）。
//
// 传入 nil 或 0 长度返回 (nil, nil)——调用方按"不校验"处理。
func compileSchema(raw []byte) (*jsonschema.Schema, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, nil
	}
	c := jsonschema.NewCompiler()
	// 用匿名 URL 注册；同一 compiler 只编一次。
	const schemaURL = "mem://compkg-config-schema.json"
	if err := c.AddResource(schemaURL, bytes.NewReader(raw)); err != nil {
		return nil, fmt.Errorf("config: add schema resource: %w", err)
	}
	s, err := c.Compile(schemaURL)
	if err != nil {
		return nil, fmt.Errorf("config: compile schema: %w", err)
	}
	return s, nil
}

// validateAgainstSchema 用编译好的 schema 校验一份 settings（map[string]any）。
//
// jsonschema/v5 要求输入是 interface{} 树（map/slice/scalar），viper.AllSettings()
// 返回的就是这种结构，可以直接传入。
//
// 返回 error 时，消息里会包含字段路径——便于在运维日志里一眼定位。
func validateAgainstSchema(s *jsonschema.Schema, settings map[string]any) error {
	if s == nil {
		return nil
	}
	if settings == nil {
		settings = map[string]any{}
	}
	if err := s.Validate(any(settings)); err != nil {
		return fmt.Errorf("config: schema validation failed: %w", prettifySchemaErr(err))
	}
	return nil
}

// prettifySchemaErr 把 jsonschema 的多行树状错误压缩成单行、突出关键路径。
//
// 默认错误形如：
//
//	jsonschema: '/server' does not validate with .../$ref/properties/server/$ref:
//	    missing properties: 'port'
//
// 压缩后：
//
//	/server: missing properties: 'port'
func prettifySchemaErr(err error) error {
	if err == nil {
		return nil
	}
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return err
	}
	var lines []string
	collectLeafErrs(ve, &lines)
	if len(lines) == 0 {
		return err
	}
	return errors.New(strings.Join(lines, "; "))
}

// collectLeafErrs 递归收集叶子级校验错误。
func collectLeafErrs(ve *jsonschema.ValidationError, out *[]string) {
	if len(ve.Causes) == 0 {
		path := ve.InstanceLocation
		if path == "" {
			path = "/"
		}
		*out = append(*out, fmt.Sprintf("%s: %s", path, ve.Message))
		return
	}
	for _, c := range ve.Causes {
		collectLeafErrs(c, out)
	}
}
