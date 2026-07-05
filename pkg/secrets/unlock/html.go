package unlock

import "html/template"

var pageTemplates = template.Must(template.New("pages").Parse(`
{{define "unlock"}}<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Dec · Bitwarden 解锁</title>
<style>
body { font-family: system-ui, sans-serif; max-width: 420px; margin: 48px auto; padding: 0 16px; color: #1a1a1a; }
h1 { font-size: 1.25rem; margin-bottom: 8px; }
p { color: #555; line-height: 1.5; }
label { display: block; margin-top: 16px; font-weight: 600; }
input { width: 100%; box-sizing: border-box; margin-top: 6px; padding: 10px; font-size: 1rem; }
button { margin-top: 20px; width: 100%; padding: 10px; font-size: 1rem; cursor: pointer; }
.error { color: #b00020; margin-top: 12px; }
</style>
</head>
<body>
<h1>Bitwarden 解锁</h1>
<p>输入主密码以继续 secrets bundle 同步。Session 仅保存在当前 Dec 进程内存中。</p>
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}
<form method="post" action="/unlock">
<label for="password">主密码</label>
<input id="password" name="password" type="password" autocomplete="current-password" required autofocus>
<button type="submit">解锁</button>
</form>
</body>
</html>{{end}}

{{define "2fa"}}<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Dec · 二次验证</title>
<style>
body { font-family: system-ui, sans-serif; max-width: 420px; margin: 48px auto; padding: 0 16px; color: #1a1a1a; }
h1 { font-size: 1.25rem; margin-bottom: 8px; }
p { color: #555; line-height: 1.5; }
label { display: block; margin-top: 16px; font-weight: 600; }
input { width: 100%; box-sizing: border-box; margin-top: 6px; padding: 10px; font-size: 1rem; letter-spacing: 0.15em; }
button { margin-top: 20px; width: 100%; padding: 10px; font-size: 1rem; cursor: pointer; }
.error { color: #b00020; margin-top: 12px; }
</style>
</head>
<body>
<h1>二次验证</h1>
<p>账户已启用 2FA，请输入 TOTP 验证码。</p>
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}
<form method="post" action="/unlock/2fa">
<label for="code">验证码</label>
<input id="code" name="code" type="text" inputmode="numeric" autocomplete="one-time-code" required autofocus>
<button type="submit">验证</button>
</form>
</body>
</html>{{end}}

{{define "success"}}<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Dec · 解锁成功</title>
<style>
body { font-family: system-ui, sans-serif; max-width: 420px; margin: 48px auto; padding: 0 16px; color: #1a1a1a; }
h1 { font-size: 1.25rem; color: #0d6a0d; }
p { color: #555; line-height: 1.5; }
</style>
</head>
<body>
<h1>解锁成功</h1>
<p>Bitwarden session 已写入当前进程内存。你可以关闭此页面，Dec 将继续同步 secrets bundle。</p>
</body>
</html>{{end}}
`))

type pageData struct {
	Error string
}
