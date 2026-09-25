package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ProviderMetaFile 是 registry 快照里提供方客观身份文件（ADR 0031）。
const ProviderMetaFile = "provider.yaml"

// ProviderMeta 随资产正文一起发布，消费方只读。
type ProviderMeta struct {
	OriginRepo string `yaml:"origin_repo,omitempty"`
	// Tags 是提供方声明的推荐标签。global 表示新机器初始化时建议默认勾选。
	// 消费方只读，不在订阅页改写。
	Tags []string `yaml:"tags,omitempty"`
	// SecretsPlane 是提供方声明的密钥平面（ADR 0035）：global = 机器根，
	// local = 项目 .secrets/。空 = 未声明（迁移期），消费侧沿用平面推导。
	SecretsPlane string `yaml:"secrets_plane,omitempty"`
}

func ProviderMetaPath(projectDir string) string {
	return filepath.Join(projectDir, ProviderMetaFile)
}

func LoadProviderMeta(projectDir string) (ProviderMeta, error) {
	var meta ProviderMeta
	data, err := os.ReadFile(ProviderMetaPath(projectDir))
	if err != nil {
		if os.IsNotExist(err) {
			return meta, nil
		}
		return meta, err
	}
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return meta, fmt.Errorf("解析 %s 失败: %w", ProviderMetaFile, err)
	}
	meta.OriginRepo = strings.TrimSpace(meta.OriginRepo)
	return meta, nil
}

func WriteProviderMeta(projectDir string, meta ProviderMeta) error {
	meta.OriginRepo = strings.TrimSpace(meta.OriginRepo)
	meta.SecretsPlane = strings.TrimSpace(meta.SecretsPlane)
	// SecretsPlane 单独出现也要写文件：身份型产品（无 origin、无 tags）的平面
	// 声明同样要随发布下发（ADR 0035）。
	if meta.OriginRepo == "" && len(meta.Tags) == 0 && meta.SecretsPlane == "" {
		return nil
	}
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(&meta)
	if err != nil {
		return err
	}
	return os.WriteFile(ProviderMetaPath(projectDir), data, 0o644)
}
