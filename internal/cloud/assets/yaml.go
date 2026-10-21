package assets

import (
	"bytes"

	"gopkg.in/yaml.v3"
)

// yamlUnmarshalStrict 用与写入一致的规则解析 YAML：未知字段直接报错。
//
// 这样资产写入时的严格校验和回滚时的快照校验使用同一套规则，
// 不会出现“写入能过、回滚报错”这种割裂。
func yamlUnmarshalStrict(data []byte, out any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	return dec.Decode(out)
}

// yamlMarshal 是写入 manifest 的统一出口。
func yamlMarshal(v any) ([]byte, error) { return yaml.Marshal(v) }
