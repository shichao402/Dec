package version

import (
	"fmt"
	"strconv"
	"strings"
)

// Compare 比较两个版本号
// 返回值: 1 表示 v1 > v2, -1 表示 v1 < v2, 0 表示相等
func Compare(v1, v2 string) int {
	// 移除 'v' 前缀
	v1 = strings.TrimPrefix(v1, "v")
	v2 = strings.TrimPrefix(v2, "v")
	
	// 处理特殊版本
	if v1 == "dev" || v1 == "unknown" {
		return -1 // dev 版本总是认为需要更新
	}
	if v2 == "dev" || v2 == "unknown" {
		return 1
	}
	
	// 分割版本号
	parts1 := strings.Split(v1, ".")
	parts2 := strings.Split(v2, ".")
	
	// 确保至少有 3 个部分（major.minor.patch）
	for len(parts1) < 3 {
		parts1 = append(parts1, "0")
	}
	for len(parts2) < 3 {
		parts2 = append(parts2, "0")
	}
	
	// 比较每个部分
	for i := 0; i < 3; i++ {
		// 提取数字部分（忽略后缀如 -rc1, -beta）
		num1 := extractNumber(parts1[i])
		num2 := extractNumber(parts2[i])
		
		if num1 > num2 {
			return 1
		}
		if num1 < num2 {
			return -1
		}
	}
	
	return 0
}

// extractNumber 从版本号部分提取数字
func extractNumber(s string) int {
	// 找到第一个非数字字符的位置
	for i, c := range s {
		if c < '0' || c > '9' {
			s = s[:i]
			break
		}
	}
	
	num, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return num
}


// NeedUpdate 检查是否需要更新
func NeedUpdate(currentVersion, latestVersion string) bool {
	return Compare(currentVersion, latestVersion) < 0
}

// FormatVersion 格式化版本号显示
func FormatVersion(version, buildTime string) string {
	if version == "" || version == "unknown" {
		version = "dev"
	}
	if buildTime != "" && buildTime != "unknown" {
		return fmt.Sprintf("%s (built at %s)", version, buildTime)
	}
	return version
}

