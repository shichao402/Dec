package agent

import "github.com/shichao402/Dec/internal/proto"

// 入口来源常量。
//
// 单独抽出来是为了让“来源由入口决定”这件事在代码里显式可见：
// 回环页面拿不到 mcp 来源，MCP 入口也拿不到 admin 来源。
const (
	protoSourceAdmin = proto.SourceAdmin
	protoSourceMCP   = proto.SourceMCP
)

// indexHTML 是本机页面。
//
// 刻意做成单页、无构建、无外链：代理是下载即跑的单文件程序，
// 页面必须能在断网时打开，也不能依赖任何 CDN。
const indexHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Dec 代理</title>
<style>
:root { color-scheme: light dark; }
* { box-sizing: border-box; }
body { margin: 0; font: 14px/1.6 -apple-system, "Segoe UI", system-ui, sans-serif; background: #f6f7f9; color: #1c1f23; }
main { max-width: 720px; margin: 0 auto; padding: 32px 20px 64px; }
h1 { font-size: 20px; margin: 0 0 4px; }
.sub { color: #6b7280; margin: 0 0 24px; font-size: 13px; }
.card { background: #fff; border: 1px solid #e5e7eb; border-radius: 10px; padding: 18px; margin-bottom: 16px; }
.card h2 { font-size: 14px; margin: 0 0 12px; letter-spacing: .02em; color: #374151; }
.row { display: flex; justify-content: space-between; gap: 12px; padding: 6px 0; border-bottom: 1px dashed #eef0f3; }
.row:last-child { border-bottom: 0; }
.k { color: #6b7280; }
.v { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; text-align: right; word-break: break-all; }
.ok { color: #0f7b3f; }
.bad { color: #b42318; }
label { display: block; font-size: 12px; color: #6b7280; margin: 10px 0 4px; }
input { width: 100%; padding: 8px 10px; border: 1px solid #d1d5db; border-radius: 7px; font: inherit; }
button { margin-top: 14px; padding: 8px 16px; border: 0; border-radius: 7px; background: #1c1f23; color: #fff; font: inherit; cursor: pointer; }
button.ghost { background: #eef0f3; color: #1c1f23; }
pre { background: #f6f7f9; border: 1px solid #e5e7eb; border-radius: 8px; padding: 10px; overflow: auto; font-size: 12px; }
.hint { font-size: 12px; color: #6b7280; margin-top: 8px; }
</style>
</head>
<body>
<main>
  <h1>Dec 代理</h1>
  <p class="sub">这台机器上的 Dec 只负责落盘，内容以云端为准。</p>

  <div class="card">
    <h2>状态</h2>
    <div id="status"><div class="row"><span class="k">加载中</span></div></div>
  </div>

  <div class="card">
    <h2>管理面</h2>
    <p class="hint">设备、资产、密钥、历史都在管理面里。页面只在本机提供，内容由代理带着设备身份去云端取。</p>
    <a href="/admin"><button class="ghost" style="margin-top:8px">打开管理面</button></a>
  </div>

  <div class="card">
    <h2>服务地址</h2>
    <label for="url">服务端地址（必须 https）</label>
    <input id="url" placeholder="https://dec.firoyang.com" autocomplete="off">
    <button onclick="configure()">保存</button>
    <p class="hint">修改服务地址会清除本机设备身份，需要重新登记。</p>
  </div>

  <div class="card" id="enrollCard">
    <h2>设备登记</h2>
    <label for="email">Bitwarden 邮箱</label>
    <input id="email" autocomplete="username">
    <label for="password">主密码</label>
    <input id="password" type="password" autocomplete="current-password">
    <label for="totp">两步验证码（如已开启）</label>
    <input id="totp" autocomplete="one-time-code">
    <label for="device">设备名</label>
    <input id="device" placeholder="留空使用主机名">
    <button onclick="enroll()">登记</button>
    <p class="hint">凭据只用于向服务端证明身份，用完即弃，不会保存在本机。</p>
  </div>

  <div class="card" id="recoverCard" style="display:none">
    <h2 id="recoverTitle">服务端需要恢复</h2>
    <p class="hint" id="recoverHint"></p>
    <label for="remail">Bitwarden 邮箱</label>
    <input id="remail" autocomplete="username">
    <label for="rpassword">主密码</label>
    <input id="rpassword" type="password" autocomplete="current-password">
    <label for="rtotp">两步验证码（如已开启）</label>
    <input id="rtotp" autocomplete="one-time-code">
    <button id="recoverBtn" onclick="recover(false)">从 Bitwarden 恢复</button>
    <button id="declareBtn" class="ghost" style="display:none" onclick="recover(true)">宣告 CA 丢失并重建</button>
    <button id="reenrollBtn" style="display:none" onclick="reenroll()">重新登记本机</button>
    <p class="hint">凭据只用于向服务端证明身份，用完即弃，不会保存在本机。</p>
  </div>

  <div class="card">
    <h2>项目</h2>
    <div id="projects"><div class="row"><span class="k">加载中</span></div></div>
    <p class="hint">项目登记决定项目平面资产落在哪个目录。</p>
  </div>
</main>
<script>
const $ = (id) => document.getElementById(id);

async function getJSON(path) {
  const r = await fetch(path, { headers: { 'Accept': 'application/json' } });
  return r.json();
}

async function postJSON(path, body) {
  const r = await fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body || {})
  });
  let data = {};
  try { data = await r.json(); } catch (e) {}
  if (!r.ok) throw new Error(data.error || ('HTTP ' + r.status));
  return data;
}

function row(k, v, cls) {
  return '<div class="row"><span class="k">' + k + '</span><span class="v ' + (cls || '') + '">' + (v || '—') + '</span></div>';
}

async function refresh() {
  try {
    const s = await getJSON('/api/status');
    let html = '';
    html += row('服务地址', s.server_url);
    html += row('代理版本', s.version);
    html += row('服务端版本', s.server_version);
    html += row('要求版本', s.required, s.aligned ? 'ok' : 'bad');
    html += row('版本状态', s.aligned ? '已对齐' : '需要更新', s.aligned ? 'ok' : 'bad');
    if (!s.aligned && s.download_url) {
      html += row('下载地址', s.download_url);
    }
    html += row('设备 ID', s.device_id);
    html += row('维护中', s.maintenance ? '是' : '否', s.maintenance ? 'bad' : 'ok');
    html += row('最近对账', s.last_sync ? new Date(s.last_sync).toLocaleString() : '—');
    html += row('当前 revision', s.revision);
    if (s.ca_state) html += row('服务端 CA', s.ca_state === 'ready' ? '正常' : s.ca_state, s.ca_state === 'ready' ? 'ok' : 'bad');
    if (s.ca_backup_pending) html += row('CA 备份', '尚未写入 Bitwarden', 'bad');
    if (s.last_error) html += row('最近错误', s.last_error, 'bad');
    $('status').innerHTML = html;
    if (s.server_url) $('url').value = s.server_url;
    $('enrollCard').style.display = s.device_id ? 'none' : 'block';
    showRecover(s);
  } catch (e) {
    $('status').innerHTML = row('读取状态失败', String(e), 'bad');
  }
  try {
    const p = await getJSON('/api/projects');
    const list = p.projects || [];
    if (!list.length) {
      $('projects').innerHTML = row('暂无登记', '—');
    } else {
      $('projects').innerHTML = list.map(x => row(x.ID, x.Path)).join('');
    }
  } catch (e) {
    $('projects').innerHTML = row('读取失败', String(e), 'bad');
  }
}

async function configure() {
  try {
    await postJSON('/api/configure', { server_url: $('url').value.trim() });
    alert('已保存。如修改了服务地址，请重新登记。');
    refresh();
  } catch (e) { alert('保存失败：' + e.message); }
}

async function enroll() {
  try {
    await postJSON('/api/enroll', {
      email: $('email').value.trim(),
      password: $('password').value,
      totp: $('totp').value.trim(),
      device_name: $('device').value.trim()
    });
    $('password').value = '';
    $('totp').value = '';
    alert('登记成功。');
    refresh();
  } catch (e) { alert('登记失败：' + e.message); }
}

// showRecover 按服务端状态与最近错误码决定显示哪个恢复入口（ADR 0052）。
let caNotInBackup = false;
function showRecover(s) {
  const code = s.error_code || '';
  const reenroll = code === 'device_needs_reenroll';
  const restore = code === 'server_ca_pending_restore' || code === 'recovery_mode' ||
    (s.ca_state && s.ca_state !== 'ready');
  $('recoverCard').style.display = (reenroll || restore || caNotInBackup) ? 'block' : 'none';
  $('reenrollBtn').style.display = reenroll ? 'inline-block' : 'none';
  $('recoverBtn').style.display = reenroll ? 'none' : 'inline-block';
  $('declareBtn').style.display = caNotInBackup ? 'inline-block' : 'none';
  if (reenroll) {
    $('recoverTitle').textContent = '服务端身份已重置';
    $('recoverHint').textContent = '服务端 CA 已重建，本机证书失效。重新登记会沿用原设备和全部订阅，本机已有文件不受影响。';
  } else if (caNotInBackup) {
    $('recoverTitle').textContent = 'Bitwarden 里找不到服务端 CA';
    $('recoverHint').textContent = '确认无法找回时可宣告丢失：将生成新 CA，所有已登记设备都需要重新登记（订阅保留）。';
  } else {
    $('recoverTitle').textContent = '服务端需要恢复';
    $('recoverHint').textContent = '服务端缺少根密钥或 CA 私钥，需要用 Bitwarden 主密码和两步验证码取回。本机已有文件保持不变。';
  }
}

function recoverCreds() {
  return {
    email: $('remail').value.trim(),
    password: $('rpassword').value,
    totp: $('rtotp').value.trim()
  };
}

function clearRecoverCreds() {
  $('rpassword').value = '';
  $('rtotp').value = '';
}

async function recover(declare) {
  if (declare && !confirm('确定宣告服务端 CA 丢失？\n\n将生成新 CA，所有已登记设备都需要重新登记（订阅保留）。此操作不可撤销。')) return;
  const body = recoverCreds();
  body.declare_ca_lost = !!declare;
  try {
    const r = await fetch('/api/recover', {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body)
    });
    const data = await r.json().catch(() => ({}));
    if (!r.ok) {
      if (data.code === 'server_ca_not_in_backup') { caNotInBackup = true; refresh(); }
      throw new Error(data.error || ('HTTP ' + r.status));
    }
    caNotInBackup = false;
    clearRecoverCreds();
    const msg = [];
    if (data.root_key_restored) msg.push('根密钥已取回');
    if (data.ca_restored) msg.push('服务端 CA 已取回，设备无需重新登记');
    if (data.ca_rebuilt) msg.push('服务端 CA 已重建，' + data.reenroll_devices + ' 台设备需要重新登记');
    if (data.enrolled) msg.push('本机证书已更新');
    alert('恢复完成：' + (msg.join('；') || '无变化'));
    refresh();
  } catch (e) { alert('恢复失败：' + e.message); }
}

async function reenroll() {
  try {
    await postJSON('/api/reenroll', recoverCreds());
    clearRecoverCreds();
    alert('重新登记成功，原设备与订阅已保留。');
    refresh();
  } catch (e) { alert('重新登记失败：' + e.message); }
}

refresh();
setInterval(refresh, 5000);
</script>
</body>
</html>
`
