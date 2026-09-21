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
	if meta.OriginRepo == "" {
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
