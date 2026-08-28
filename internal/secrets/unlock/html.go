package unlock

import "html/template"

var pageTemplates = template.Must(template.New("pages").Parse(`
{{define "unlock-styles"}}
:root {
  --brand: #175DDC;
  --brand-hover: #134bb8;
  --brand-ring: rgba(23, 93, 220, 0.35);
  --bg: #f4f5f7;
  --card: #ffffff;
  --text: #1a1f36;
  --text-muted: #697386;
  --border: #d8dee9;
  --input-bg: #ffffff;
  --error-bg: #fef2f2;
  --error-border: #fecaca;
  --error-text: #b91c1c;
  --success: #059669;
  --success-bg: #ecfdf5;
  --radius: 10px;
  --shadow: 0 4px 24px rgba(0, 0, 0, 0.06), 0 1px 2px rgba(0, 0, 0, 0.04);
}
*, *::before, *::after { box-sizing: border-box; }
html { -webkit-text-size-adjust: 100%; }
body {
  margin: 0;
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px 16px;
  font-family: system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
  font-size: 16px;
  line-height: 1.5;
  color: var(--text);
  background: var(--bg);
}
.page {
  width: 100%;
  max-width: 780px;
}
.card {
  background: var(--card);
  border-radius: 12px;
  box-shadow: var(--shadow);
  padding: 32px 28px;
}
@media (max-width: 420px) {
  .card { padding: 28px 20px; }
}
.brand {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 24px;
}
.brand-mark {
  width: 36px;
  height: 36px;
  border-radius: 9px;
  background: linear-gradient(135deg, var(--brand) 0%, #2d7ff9 100%);
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  font-weight: 700;
  font-size: 0.875rem;
  letter-spacing: -0.02em;
  flex-shrink: 0;
}
.brand-name {
  font-size: 0.8125rem;
  font-weight: 600;
  color: var(--text-muted);
  letter-spacing: 0.02em;
  text-transform: uppercase;
}
h1 {
  margin: 0 0 8px;
  font-size: 1.375rem;
  font-weight: 600;
  letter-spacing: -0.02em;
  line-height: 1.3;
}
.lead {
  margin: 0 0 24px;
  font-size: 0.9375rem;
  color: var(--text-muted);
  line-height: 1.55;
}
.alert {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  margin-bottom: 20px;
  padding: 12px 14px;
  border-radius: var(--radius);
  font-size: 0.875rem;
  line-height: 1.45;
}
.alert-error {
  background: var(--error-bg);
  border: 1px solid var(--error-border);
  color: var(--error-text);
}
.alert-icon {
  flex-shrink: 0;
  width: 18px;
  height: 18px;
  margin-top: 1px;
}
.field { margin-bottom: 18px; }
.field label {
  display: block;
  margin-bottom: 6px;
  font-size: 0.875rem;
  font-weight: 500;
  color: var(--text);
}
.field input {
  display: block;
  width: 100%;
  margin: 0;
  padding: 11px 13px;
  font-family: inherit;
  font-size: 1rem;
  line-height: 1.4;
  color: var(--text);
  background: var(--input-bg);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  appearance: none;
  -webkit-appearance: none;
  transition: border-color 0.15s, box-shadow 0.15s;
}
.field input[inputmode="numeric"] { letter-spacing: 0.12em; }
.field input:focus {
  outline: none;
  border-color: var(--brand);
  box-shadow: 0 0 0 3px var(--brand-ring);
}
.field input:-webkit-autofill,
.field input:-webkit-autofill:hover,
.field input:-webkit-autofill:focus {
  -webkit-text-fill-color: var(--text);
  -webkit-box-shadow: 0 0 0 1000px var(--input-bg) inset;
  box-shadow: 0 0 0 1000px var(--input-bg) inset;
  border-color: var(--border);
}
.field input:-webkit-autofill:focus {
  border-color: var(--brand);
  box-shadow: 0 0 0 1000px var(--input-bg) inset, 0 0 0 3px var(--brand-ring);
}
.decoy-field {
  position: absolute;
  left: -9999px;
  width: 1px;
  height: 1px;
  opacity: 0;
  pointer-events: none;
}
.checkbox-row {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  margin: 4px 0 22px;
  cursor: pointer;
  user-select: none;
}
.checkbox-row input[type="checkbox"] {
  width: 18px;
  height: 18px;
  margin: 2px 0 0;
  flex-shrink: 0;
  accent-color: var(--brand);
  cursor: pointer;
}
.checkbox-text {
  font-size: 0.875rem;
  line-height: 1.45;
  color: var(--text);
}
.checkbox-hint {
  display: block;
  margin-top: 2px;
  font-size: 0.8125rem;
  color: var(--text-muted);
}
.btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 100%;
  padding: 11px 16px;
  font-size: 0.9375rem;
  font-weight: 600;
  line-height: 1.4;
  color: #fff;
  background: var(--brand);
  border: none;
  border-radius: var(--radius);
  cursor: pointer;
  transition: background 0.15s, opacity 0.15s;
}
.btn:hover:not(:disabled) { background: var(--brand-hover); }
.btn:focus-visible {
  outline: none;
  box-shadow: 0 0 0 3px var(--brand-ring);
}
.btn:disabled {
  opacity: 0.65;
  cursor: wait;
}
.spinner {
  display: inline-block;
  width: 1em;
  height: 1em;
  margin-right: 0.45em;
  border: 2px solid rgba(255, 255, 255, 0.35);
  border-top-color: #fff;
  border-radius: 50%;
  animation: spin 0.6s linear infinite;
  vertical-align: -0.15em;
}
@keyframes spin { to { transform: rotate(360deg); } }
.success-icon {
  width: 56px;
  height: 56px;
  margin: 0 auto 20px;
  border-radius: 50%;
  background: var(--success-bg);
  display: flex;
  align-items: center;
  justify-content: center;
}
.success-icon svg { width: 28px; height: 28px; color: var(--success); }
.success-body { text-align: center; }
.success-body h1 { color: var(--success); margin-bottom: 10px; }
.countdown {
  margin: 20px 0 0;
  padding: 14px 16px;
  border-radius: var(--radius);
  background: var(--bg);
  font-size: 0.875rem;
  color: var(--text-muted);
}
.countdown strong {
  font-weight: 600;
  color: var(--text);
  font-variant-numeric: tabular-nums;
}
.manual-close {
  margin-top: 16px;
  font-size: 0.875rem;
  color: var(--text-muted);
}
.request-warning {
  margin: 0 0 16px;
  padding: 12px 14px;
  border: 1px solid #f4c86a;
  border-radius: var(--radius);
  background: #fffbeb;
  color: #7c5200;
  font-size: 0.875rem;
}
.request-panel {
  margin: 0 0 24px;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: #f8fafc;
  overflow: hidden;
}
.request-panel summary {
  padding: 12px 14px;
  cursor: pointer;
  font-size: 0.875rem;
  font-weight: 600;
  overflow-wrap: anywhere;
}
.request-details {
  padding: 0 14px 14px;
  border-top: 1px solid var(--border);
}
.request-grid {
  display: grid;
  grid-template-columns: 130px minmax(0, 1fr);
  margin: 0;
  font-size: 0.8125rem;
}
.request-grid dt,
.request-grid dd {
  margin: 0;
  padding: 7px 0;
  border-bottom: 1px solid #e5e7eb;
}
.request-grid dt { color: var(--text-muted); }
.request-grid dd {
  font-family: ui-monospace, SFMono-Regular, Consolas, "Liberation Mono", monospace;
  overflow-wrap: anywhere;
}
.stack-title {
  margin: 14px 0 6px;
  font-size: 0.8125rem;
  font-weight: 600;
}
.call-stack {
  max-height: 360px;
  margin: 0;
  padding: 12px;
  overflow: auto;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  border-radius: 7px;
  background: #111827;
  color: #e5e7eb;
  font: 0.75rem/1.55 ui-monospace, SFMono-Regular, Consolas, "Liberation Mono", monospace;
}
{{end}}

{{define "request-details"}}
<p class="request-warning"><strong>先确认来源：</strong>只有当下面的程序和操作是你刚刚发起的，才输入主密码。来源不明时直接关闭本页。</p>
<details class="request-panel" open>
  <summary>认证请求 {{.Request.ID}} · {{.Request.Source}}</summary>
  <div class="request-details">
    <dl class="request-grid">
      <dt>请求 ID</dt><dd>{{.Request.ID}}</dd>
      <dt>业务标识</dt><dd>{{.Request.Source}}</dd>
      <dt>门面 / 客户端</dt><dd>{{.Request.Facade}} / {{.Request.ClientID}}</dd>
      <dt>服务操作</dt><dd>{{.Request.Operation}}</dd>
      <dt>Operation ID</dt><dd>{{.Request.OperationID}}</dd>
      <dt>工作区平面</dt><dd>{{.Request.WorkspacePlane}}</dd>
      <dt>项目根</dt><dd>{{.Request.ProjectRoot}}</dd>
      <dt>发起时间</dt><dd>{{.Request.RequestedAt}}</dd>
      <dt>程序</dt><dd>{{.Request.Executable}}</dd>
      <dt>进程</dt><dd>PID {{.Request.PID}} · PPID {{.Request.PPID}}</dd>
      <dt>父进程</dt><dd>{{.Request.ParentProcess}}</dd>
      <dt>工作目录</dt><dd>{{.Request.WorkingDir}}</dd>
      <dt>主机名</dt><dd>{{.Request.Hostname}}</dd>
      <dt>本机 IP</dt><dd>{{.Request.IPs}}</dd>
      <dt>本机 MAC</dt><dd>{{.Request.MACs}}</dd>
      <dt>运行时</dt><dd>{{.Request.GoVersion}}</dd>
    </dl>
    <div class="stack-title">调用栈（触发 web unlock 的当前 goroutine）</div>
    <pre class="call-stack">{{.Request.CallStack}}</pre>
  </div>
</details>
{{end}}

{{define "unlock"}}<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Dec · Bitwarden 解锁</title>
<style>{{template "unlock-styles" .}}</style>
</head>
<body>
<div class="page">
  <div class="card">
    <div class="brand">
      <div class="brand-mark" aria-hidden="true">D</div>
      <span class="brand-name">Dec · Bitwarden</span>
    </div>
    <h1>Bitwarden 解锁</h1>
    <p class="lead">输入 Bitwarden 邮箱与主密码以继续 secrets bundle 同步。Session 仅保存在当前 Dec 进程内存中。</p>
    {{template "request-details" .}}
    {{if .Error}}<div class="alert alert-error error" role="alert">
      <svg class="alert-icon" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true"><path fill-rule="evenodd" d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-7 4a1 1 0 11-2 0 1 1 0 012 0zm-1-9a1 1 0 00-1 1v4a1 1 0 102 0V6a1 1 0 00-1-1z" clip-rule="evenodd"/></svg>
      <span>{{.Error}}</span>
    </div>{{end}}
    <form id="unlock-form" method="post" action="/unlock" autocomplete="off" data-form-type="other">
      <input class="decoy-field" type="password" name="password_decoy" autocomplete="new-password" tabindex="-1" aria-hidden="true">
      <div class="field">
        <label for="email">邮箱</label>
        <input class="input" id="email" name="email" type="email" autocomplete="username" required{{if .Email}} value="{{.Email}}"{{end}}{{if not .Email}} autofocus{{end}} readonly onfocus="this.removeAttribute('readonly')" data-lpignore="true" data-1p-ignore data-form-type="other">
      </div>
      <div class="field">
        <label for="password">主密码</label>
        <input class="input" id="password" name="password" type="password" autocomplete="current-password" required{{if .Email}} autofocus{{end}} readonly onfocus="this.removeAttribute('readonly')" data-lpignore="true" data-1p-ignore data-form-type="other">
      </div>
      <button id="submit-btn" class="btn" type="submit">解锁</button>
    </form>
  </div>
</div>
<script>
(function () {
  var form = document.getElementById('unlock-form');
  var btn = document.getElementById('submit-btn');
  var emailInput = document.getElementById('email');
  var passwordInput = document.getElementById('password');
  var loading = '正在解锁…';
  function setLoading() {
    btn.disabled = true;
    btn.setAttribute('aria-busy', 'true');
    btn.innerHTML = '<span class="spinner" aria-hidden="true"></span>' + loading;
  }
  form.addEventListener('submit', setLoading);
  function submitOnEnter(input) {
    input.addEventListener('keydown', function (e) {
      if (e.key === 'Enter' && !btn.disabled) {
        e.preventDefault();
        if (form.reportValidity()) form.requestSubmit();
      }
    });
  }
  submitOnEnter(emailInput);
  submitOnEnter(passwordInput);
})();
</script>
</body>
</html>{{end}}

{{define "2fa"}}<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Dec · 二次验证</title>
<style>{{template "unlock-styles" .}}</style>
</head>
<body>
<div class="page">
  <div class="card">
    <div class="brand">
      <div class="brand-mark" aria-hidden="true">D</div>
      <span class="brand-name">Dec · Bitwarden</span>
    </div>
    <h1>二次验证</h1>
    <p class="lead">账户已启用 2FA，请输入 TOTP 验证码。</p>
    {{template "request-details" .}}
    {{if .Error}}<div class="alert alert-error error" role="alert">
      <svg class="alert-icon" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true"><path fill-rule="evenodd" d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-7 4a1 1 0 11-2 0 1 1 0 012 0zm-1-9a1 1 0 00-1 1v4a1 1 0 102 0V6a1 1 0 00-1-1z" clip-rule="evenodd"/></svg>
      <span>{{.Error}}</span>
    </div>{{end}}
    <form id="2fa-form" method="post" action="/unlock/2fa" autocomplete="off" data-form-type="other">
      <input class="decoy-field" type="text" name="username" autocomplete="username" tabindex="-1" aria-hidden="true">
      <input class="decoy-field" type="password" name="password_decoy" autocomplete="new-password" tabindex="-1" aria-hidden="true">
      <div class="field">
        <label for="code">验证码</label>
        <input class="input" id="code" name="code" type="text" inputmode="numeric" autocomplete="one-time-code" required autofocus readonly onfocus="this.removeAttribute('readonly')" data-lpignore="true" data-1p-ignore data-form-type="other">
      </div>
      <label class="checkbox-row" for="remember">
        <input id="remember" name="remember" type="checkbox" value="1" checked data-lpignore="true" data-1p-ignore>
        <span class="checkbox-text">
          记住此设备
          <span class="checkbox-hint">30 天内免二次验证</span>
        </span>
      </label>
      <button id="submit-btn" class="btn" type="submit">验证</button>
    </form>
  </div>
</div>
<script>
(function () {
  var form = document.getElementById('2fa-form');
  var btn = document.getElementById('submit-btn');
  var input = document.getElementById('code');
  var loading = '正在验证…';
  function setLoading() {
    btn.disabled = true;
    btn.setAttribute('aria-busy', 'true');
    btn.innerHTML = '<span class="spinner" aria-hidden="true"></span>' + loading;
  }
  form.addEventListener('submit', setLoading);
  input.addEventListener('keydown', function (e) {
    if (e.key === 'Enter' && !btn.disabled) {
      e.preventDefault();
      if (form.reportValidity()) form.requestSubmit();
    }
  });
})();
</script>
</body>
</html>{{end}}

{{define "success"}}<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Dec · 解锁成功</title>
<style>{{template "unlock-styles" .}}</style>
</head>
<body>
<div class="page">
  <div class="card success-body">
    <div class="success-icon" aria-hidden="true">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
        <polyline points="20 6 9 17 4 12"></polyline>
      </svg>
    </div>
    <h1>解锁成功</h1>
    <p class="lead">Bitwarden session 已写入当前进程内存，Dec 将继续同步 secrets bundle。</p>
    <p class="lead">请求 ID：<code>{{.Request.ID}}</code> · 来源：<code>{{.Request.Source}}</code></p>
    <p class="countdown" id="countdown">窗口将在 <strong><span id="seconds">10</span></strong> 秒后自动关闭…</p>
    <p class="manual-close" id="manual-close" hidden>可手动关闭此标签页。</p>
  </div>
</div>
<script>
(function () {
  var secondsEl = document.getElementById('seconds');
  var countdownEl = document.getElementById('countdown');
  var manualEl = document.getElementById('manual-close');
  var remaining = 10;
  var timer = setInterval(function () {
    remaining -= 1;
    if (remaining > 0) {
      secondsEl.textContent = String(remaining);
      return;
    }
    clearInterval(timer);
    countdownEl.textContent = '正在关闭窗口…';
    window.close();
    setTimeout(function () {
      if (!window.closed) {
        countdownEl.hidden = true;
        manualEl.hidden = false;
      }
    }, 300);
  }, 1000);
})();
</script>
</body>
</html>{{end}}
`))

type pageData struct {
	Error   string
	Email   string
	Request RequestDetails
}
