package assets

import (
	"testing"
)

// TestScanToleratesTemplateAndDocFalsePositives 锁住明文扫描不该挡的两类内容。
//
// 这两类都是迁移存量时真实撞上的：正常资产被规则挡住，用户只能改写内容
// 或绕过检查，两者都比漏掉一个启发式判断更糟。规则要能放行它们，
// 同时下面的正向用例必须仍然命中，不能一放行就全放过。
func TestScanToleratesTemplateAndDocFalsePositives(t *testing.T) {
	fine := map[string]string{
		"蓝盾模板变量": "git clone \"https://${{settings.my_git_token.username}}:${{settings.my_git_token.password}}@git.woa.com/{group}/{repo}.git\"\n",
		"类型映射表":  `"password": "vuex-input",`,
		"帮助文案":   "cdb_account_password: \"修改密码请用控制台或 CDB API（CDB_PASSWORD 为 MySQL 账号密码）\",",
		"普通文档":   "把 password 写进 mcp.json 是不允许的，请改用密钥引用。",
	}
	for name, content := range fine {
		err := scanForPlaintextSecrets([]AssetFile{{Path: "a.md", Content: []byte(content)}})
		if err != nil {
			t.Errorf("%s: 不该被判定为明文密钥，实际 %v", name, err)
		}
	}
}

// TestScanStillCatchesRealSecrets 是上一条的反向约束：
// 放行误报之后，真正的密钥仍然必须命中。
func TestScanStillCatchesRealSecrets(t *testing.T) {
	bad := map[string]string{
		"连接串":         "redis://user:supersecret@host:6379",
		"内联字段":        `api_secret: "abcdefgh12345678"`,
		"内联 password": `{"password": "P@ssw0rd123"}`,
		"GitHub 令牌":   "ghp_abcdefghijklmnopqrstuvwxyz0123456789",
		"私钥块":         "-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n",
	}
	for name, content := range bad {
		err := scanForPlaintextSecrets([]AssetFile{{Path: "a.md", Content: []byte(content)}})
		if err == nil {
			t.Errorf("%s: 应被判定为明文密钥，实际通过了", name)
		}
	}
}
