package version

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

// GitHubRelease GitHub Release 信息
type GitHubRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
}

// GetLatestRelease 获取 GitHub 仓库的最新 Release
func GetLatestRelease(owner, repo string) (*GitHubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)
	
	// 创建请求并添加 User-Agent 头
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("User-Agent", "CursorToolset/1.0")
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("未找到任何 Release")
	}
	
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("请求失败，状态码: %d", resp.StatusCode)
	}
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	
	var release GitHubRelease
	if err := json.Unmarshal(body, &release); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	
	return &release, nil
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

