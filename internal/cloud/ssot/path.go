package ssot

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// 资产命名与路径校验的规则集中在这里，写入链路和渲染链路共用。
//
// 这些校验只用于“防止写错”，不是安全边界：真正的边界是服务端和它的部署链路。

var (
	// bundleRe 约束 bundle 名：小写字母、数字、点、下划线、短横，且必须以字母数字开头。
	bundleRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	// itemRe 约束条目名。
	itemRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	// entryPrefix 是 Dec 在共享配置文件里托管条目的统一前缀。
	entryPrefix = "dec-"
)

// ErrInvalidName 表示命名不合规。
var ErrInvalidName = errors.New("invalid name")

// ValidateBundle 校验 bundle 名。
func ValidateBundle(name string) error {
	if !bundleRe.MatchString(name) {
		return fmt.Errorf("%w: bundle %q 必须匹配 %s", ErrInvalidName, name, bundleRe.String())
	}
	if name == NamespaceCloud {
		return fmt.Errorf("%w: bundle %q 保留给 Dec 内置内容", ErrInvalidName, name)
	}
	return nil
}

// ValidateItemName 校验条目名。
func ValidateItemName(name string) error {
	if !itemRe.MatchString(name) {
		return fmt.Errorf("%w: 条目名 %q 必须匹配 %s", ErrInvalidName, name, itemRe.String())
	}
	return nil
}

// EntryKey 返回共享配置文件里这条条目使用的键名。
func EntryKey(name string) string { return entryPrefix + name }

// HasEntryPrefix 报告一个键名是否由 Dec 托管。
func HasEntryPrefix(key string) bool { return strings.HasPrefix(key, entryPrefix) }

// SafeRelPath 校验仓库内相对路径，拒绝绝对路径、上跳和空段。
func SafeRelPath(p string) error {
	if p == "" {
		return fmt.Errorf("%w: 路径为空", ErrInvalidName)
	}
	if strings.ContainsRune(p, '\\') {
		return fmt.Errorf("%w: 路径 %q 不允许反斜杠", ErrInvalidName, p)
	}
	if path.IsAbs(p) || filepath.IsAbs(p) {
		return fmt.Errorf("%w: 路径 %q 不允许绝对路径", ErrInvalidName, p)
	}
	clean := path.Clean(p)
	if clean != p {
		return fmt.Errorf("%w: 路径 %q 未规范化，应为 %q", ErrInvalidName, p, clean)
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("%w: 路径 %q 不允许上跳", ErrInvalidName, p)
	}
	for _, seg := range strings.Split(clean, "/") {
		if seg == "" || seg == "." {
			return fmt.Errorf("%w: 路径 %q 含空段", ErrInvalidName, p)
		}
	}
	return nil
}

// AssetDir 返回仓库内某个 bundle 的目录。
func AssetDir(bundle string) string { return path.Join(DirAssets, bundle) }

// SecretFile 返回仓库内某个密钥的路径。
func SecretFile(name string) string { return path.Join(DirSecrets, name+".json") }
