package agent

// 管理面的脚本（需求 14.1、14.2、14.3）。
//
// 全部逻辑都在这一个常量里，不拆模块、不引构建工具：代理是下载即跑的单文件
// 程序，页面必须能离线打开。离线时也要能看，所以所有请求失败都只降级成
// “读不到”，不阻塞界面。
//
// 三条贯穿全篇的约束：
//
//  1. 所有写操作都先弹二次确认，确认框里写明将要发生什么（需求 14.3）；
//  2. 明文只在显式点击“查看”后出现，且必须填理由，理由会进审计（需求 8.6）；
//  3. 页面自己不做权限判断，只按服务端返回的错误展示——权限的判定点在服务端，
//     在浏览器里复制一份规则只会让两边慢慢走偏。
const adminJS = `'use strict';

const $ = (id) => document.getElementById(id);

// 当前选中的 bundle 与它的完整内容。保存单个文件时要把整包写回，
// 因为 write_asset 是整体替换语义。
let currentBundle = null;
let currentAsset = null;

// ---------- 基础设施 ----------

// api 走本机代理的管理面转发，来源由入口固定为 admin。
//
// 这样页面自己不需要任何凭据：设备身份在代理里，浏览器只是个显示器。
async function api(method, payload) {
  const r = await fetch('/api/call', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ method: method, payload: payload || {} })
  });
  let data = null;
  try { data = await r.json(); } catch (e) { data = null; }
  if (!r.ok) {
    throw new Error((data && data.error) || ('HTTP ' + r.status));
  }
  return data || {};
}

// esc 转义 HTML。所有来自云端的内容都可能是用户自己写的资产文本，
// 直接拼进 innerHTML 会让一个引号把页面结构搞坏。
function esc(v) {
  return String(v == null ? '' : v)
    .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}

function row(k, v, cls) {
  return '<div class="row"><span class="k">' + esc(k) + '</span><span class="v ' +
    (cls || '') + '">' + esc(v == null || v === '' ? '—' : v) + '</span></div>';
}

function when(v) {
  if (!v) return '—';
  const d = new Date(v);
  if (isNaN(d.getTime()) || d.getFullYear() < 2000) return '—';
  return d.toLocaleString();
}

function empty(text) {
  return '<div class="empty">' + esc(text) + '</div>';
}

function err(text) {
  return '<div class="err">' + esc(text) + '</div>';
}

// confirmDo 是唯一的高危操作入口。
//
// 每一个会改变云端状态的按钮都必须经过它：二次确认不是可选的装饰，
// 而是需求 14.3 的硬性要求。
function confirmDo(title, body, fn, opts) {
  const o = opts || {};
  $('cfTitle').textContent = title;
  $('cfBody').textContent = body || '';
  $('cfExtra').innerHTML = o.extraHTML || '';
  $('cfError').innerHTML = '';
  const ok = $('cfOK');
  ok.textContent = o.okText || '确认';
  ok.className = 'pri' + (o.danger ? ' hot' : '');
  // 换掉节点而不是改 onclick，避免上一次的处理器残留下来执行了别的事。
  const fresh = ok.cloneNode(true);
  ok.parentNode.replaceChild(fresh, ok);
  fresh.addEventListener('click', async () => {
    fresh.disabled = true;
    $('cfError').innerHTML = '';
    try {
      await fn();
      closeConfirm();
    } catch (e) {
      // 错误单独占一行：失败时不能覆盖输入框，否则用户要点重试就得全部重填。
      $('cfError').innerHTML = err(e.message);
      fresh.disabled = false;
    }
  });
  $('confirmDlg').showModal();
}

function closeConfirm() {
  const d = $('confirmDlg');
  if (d.open) d.close();
}

// banner 显示全局性的状态提示（离线、版本不对齐、维护中）。
function banner(text, bad) {
  const el = $('banner');
  if (!text) { el.innerHTML = ''; return; }
  el.innerHTML = '<div class="banner' + (bad ? ' bad' : '') + '">' + esc(text) + '</div>';
}

// ---------- 导航 ----------

function show(page) {
  document.querySelectorAll('#nav button').forEach((b) => {
    b.classList.toggle('on', b.dataset.page === page);
  });
  document.querySelectorAll('main section').forEach((s) => {
    s.classList.toggle('on', s.id === 'page-' + page);
  });
  if (page === 'overview') loadOverview();
  if (page === 'devices') loadDevices();
  if (page === 'assets') loadBundles();
  if (page === 'secrets') loadSecrets();
  if (page === 'history') loadHistory();
  if (page === 'projects') loadProjects();
}

// ---------- 概览 ----------

async function loadOverview() {
  // 本地状态走代理自己的 /api/status：它不依赖云端，离线时也能看。
  try {
    const r = await fetch('/api/status', { headers: { 'Accept': 'application/json' } });
    const s = await r.json();
    let html = '';
    html += row('服务地址', s.server_url);
    html += row('代理版本', s.version);
    html += row('设备 ID', s.device_id || '未登记');
    html += row('版本对齐', s.aligned ? '已对齐' : '需要更新', s.aligned ? 'ok' : 'bad');
    html += row('维护中', s.maintenance ? '是' : '否', s.maintenance ? 'bad' : 'ok');
    html += row('最近对账', when(s.last_sync));
    html += row('当前 revision', s.revision);
    if (s.last_error) html += row('最近错误', s.last_error, 'bad');
    $('local').innerHTML = html;
    if (!s.device_id) {
      banner('这台机器还没有登记，先在本机首页完成登记。', true);
    } else if (!s.aligned) {
      banner('代理版本与服务端要求不一致，请先更新到 ' + (s.required || '要求版本') + '。', true);
    } else if (s.maintenance) {
      banner('服务端正在部署，此时不能写入。', false);
    } else {
      banner('');
    }
  } catch (e) {
    $('local').innerHTML = err('读取本机状态失败：' + e.message);
  }

  // 云端状态要看设备身份，离线时读不到是正常的。
  try {
    const st = await api('status');
    let html = '';
    html += row('服务端版本', st.server_version);
    html += row('要求版本', st.required_version);
    html += row('协议版本', st.protocol_version);
    html += row('维护中', st.maintenance ? '是' : '否', st.maintenance ? 'bad' : 'ok');
    html += row('初始化入口', st.bootstrap_open ? '开放（尚未完成初始化）' : '已关闭');
    html += row('恢复模式', st.recovery_mode ? '是' : '否', st.recovery_mode ? 'bad' : 'ok');
    html += row('当前 revision', st.revision);
    html += row('根密钥 ID', st.root_key_id);
    // 冷备校验时间只在初始化、轮换、恢复时更新，服务不持有 Bitwarden 凭据（需求 11.5）。
    html += row('冷备最后校验', when(st.backup_checked_at));
    $('cloud').innerHTML = html;
  } catch (e) {
    $('cloud').innerHTML = err('读取云端状态失败：' + e.message);
  }
}

// ---------- 设备 ----------

async function loadDevices() {
  // 要求版本只在 status 里，设备列表本身不带。取一次给“对齐”列用。
  let required = '';
  try {
    const st = await api('status');
    required = st.required_version || '';
  } catch (e) {
    // 读不到要求版本不影响设备列表本身，对齐列显示成未知即可。
  }

  try {
    const d = await api('devices');
    const list = d.devices || [];
    if (!list.length) {
      $('devices').innerHTML = empty('还没有登记任何设备。');
    } else {
      let html = '<table><thead><tr><th>设备</th><th>类型</th><th>状态</th><th>差异</th>' +
        '<th>版本</th><th>对齐</th><th>最近在线</th><th>证书到期</th><th></th></tr></thead><tbody>';
      for (const x of list) {
        const kind = x.kind === 'mine' ? '我的设备' : '只接收';
        const state = x.revoked ? '<span class="bad">已吊销</span>'
          : (x.online ? '<span class="ok">在线</span>' : '<span class="warn">离线</span>');
        const drift = x.drift > 0 ? '<span class="warn">' + x.drift + ' 条</span>' : '—';
        // 未对齐的设备拿不到下发，所以这一列要显眼：它是“为什么这台机器没更新”的答案。
        let align = '—';
        if (x.reported_version && required) {
          align = x.reported_version === required ? '<span class="ok">已对齐</span>'
            : '<span class="bad">需更新</span>';
        }
        let ops = '';
        if (!x.revoked) {
          const next = x.kind === 'mine' ? 'receiver' : 'mine';
          const nextText = next === 'mine' ? '改为我的设备' : '改为只接收';
          ops += '<button class="act" onclick="setKind(&quot;' + esc(x.id) + '&quot;,&quot;' + next +
            '&quot;,&quot;' + esc(x.name || x.id) + '&quot;)">' + nextText + '</button> ';
          ops += '<button class="danger" onclick="revoke(&quot;' + esc(x.id) + '&quot;,&quot;' +
            esc(x.name || x.id) + '&quot;)">吊销</button>';
        } else {
          ops = '<span class="tag">已吊销</span>';
        }
        html += '<tr><td><div>' + esc(x.name || '（未命名）') + '</div><div class="mono">' +
          esc(x.id) + '</div></td><td>' + esc(kind) + '</td><td>' + state + '</td><td>' + drift +
          '</td><td class="mono">' + esc(x.reported_version || '—') + '</td><td>' + align +
          '</td><td>' + esc(when(x.last_seen)) + '</td><td>' + esc(when(x.cert_not_after)) +
          '</td><td>' + ops + '</td></tr>';
      }
      html += '</tbody></table>';
      $('devices').innerHTML = html;
    }
  } catch (e) {
    $('devices').innerHTML = err('读取设备失败：' + e.message);
  }

  try {
    const r = await api('rotation_list');
    const items = r.items || [];
    if (!items.length) {
      $('rotation').innerHTML = empty('没有待轮换的密钥。');
    } else {
      let html = '<table><thead><tr><th>密钥</th><th>涉及设备</th><th>状态</th></tr></thead><tbody>';
      for (const it of items) {
        html += '<tr><td class="mono">' + esc(it.secret) + '</td><td class="mono">' +
          esc((it.devices || []).join('、')) + '</td><td>' +
          (it.rotated ? '<span class="ok">已轮换 ' + esc(when(it.rotated_at)) + '</span>'
            : '<span class="bad">待轮换</span>') + '</td></tr>';
      }
      html += '</tbody></table>';
      $('rotation').innerHTML = html;
    }
  } catch (e) {
    $('rotation').innerHTML = err('读取轮换清单失败：' + e.message);
  }

  // 覆盖记录：对账时云端发现本地内容被改过，会记在这里（需求 6.2）。
  try {
    const o = await api('overrides');
    const list = o.overrides || [];
    if (!list.length) {
      $('overrides').innerHTML = empty('没有本地改动记录。');
    } else {
      let html = '<table><thead><tr><th>条目</th><th>落点</th><th>发现时间</th></tr></thead><tbody>';
      for (const x of list) {
        html += '<tr><td class="mono">' + esc(x.id) + '</td><td class="mono">' +
          esc((x.paths || []).join('、')) + '</td><td>' + esc(when(x.detected_at)) + '</td></tr>';
      }
      html += '</tbody></table>';
      $('overrides').innerHTML = html;
    }
  } catch (e) {
    $('overrides').innerHTML = err('读取覆盖记录失败：' + e.message);
  }
}

function setKind(id, kind, name) {
  const text = kind === 'mine' ? '我的设备' : '只接收设备';
  confirmDo('修改设备类型', '把「' + name + '」改为' + text + '？', async () => {
    await api('set_device_kind', { device_id: id, kind: kind });
    await loadDevices();
  });
}

function revoke(id, name) {
  // 吊销是不可撤销的，所以确认框里要写清楚收拾残局的办法。
  confirmDo('吊销设备', '吊销「' + name + '」后，这台设备立刻失去访问权，并且不再接收下发。' +
    '它曾经拿到过的密钥会进入轮换清单，需要你自己换新值。这个操作不可撤销。',
    async () => {
      await api('revoke_device', { device_id: id, reason: '在管理面吊销' });
      await loadDevices();
    }, { danger: true, okText: '吊销' });
}

// ---------- 资产 ----------

async function loadBundles() {
  try {
    const d = await api('list_assets');
    const list = d.bundles || [];
    if (!list.length) {
      $('bundles').innerHTML = empty('还没有任何 bundle。');
      return;
    }
    let html = '<table><thead><tr><th>Bundle</th><th>条目数</th><th>类型</th><th></th></tr></thead><tbody>';
    for (const b of list) {
      html += '<tr><td class="mono">' + esc(b.name) + '</td><td>' + esc(b.items || 0) +
        '</td><td>' + esc((b.kinds || []).join('、')) + '</td><td>' +
        '<button class="act" onclick="openBundle(&quot;' + esc(b.name) + '&quot;)">浏览</button>' +
        '</td></tr>';
    }
    html += '</tbody></table>';
    $('bundles').innerHTML = html;
  } catch (e) {
    $('bundles').innerHTML = err('读取资产列表失败：' + e.message);
  }
}

// openNewBundle 新建一个空 bundle。
//
// 只写一个空 manifest：条目由用户先放文件、再由服务端按文件树推断，
// 也可以直接在编辑框里改 manifest.yaml 显式声明。
function openNewBundle() {
  confirmDo('新建 bundle', 'bundle 名会出现在资产里，也是云端目录名，只能用字母、数字和连字符。',
    async () => {
      const name = ($('cfName').value || '').trim();
      if (!name) throw new Error('名称不能为空');
      await api('write_asset', {
        bundle: name,
        items: [],
        manifest: encode('name: ' + name + '\nitems: []\n'),
        message: '新建 bundle ' + name
      });
      await loadBundles();
      await openBundle(name);
    }, {
      okText: '创建',
      extraHTML: '<label for="cfName">bundle 名</label>' +
        '<input id="cfName" placeholder="例如 woa">'
    });
}

async function openBundle(name) {
  try {
    const a = await api('read_asset', { bundle: name });
    currentBundle = name;
    currentAsset = a;
    $('assetDetailCard').style.display = 'block';
    $('assetTitle').textContent = name + '（revision ' + (a.revision || '—') + '）';

    let html = '';
    if ((a.items || []).length) {
      html += '<table><thead><tr><th>条目</th><th>类型</th><th>路径</th><th>平面</th></tr></thead><tbody>';
      for (const it of a.items) {
        html += '<tr><td class="mono">' + esc(it.name) + '</td><td>' + esc(it.kind) +
          '</td><td class="mono">' + esc(it.path) + '</td><td>' +
          esc((it.planes || []).join('、')) + '</td></tr>';
      }
      html += '</tbody></table>';
    }
    $('assetDetail').innerHTML = html;

    // 文件下拉里只放真实文件，manifest.yaml 单独作为一项，方便直接改声明。
    const sel = $('assetFile');
    sel.innerHTML = '';
    for (const f of (a.files || [])) {
      const o = document.createElement('option');
      o.value = f.path;
      o.textContent = f.path;
      sel.appendChild(o);
    }
    if (a.manifest) {
      const o = document.createElement('option');
      o.value = 'manifest.yaml';
      o.textContent = 'manifest.yaml（声明）';
      sel.appendChild(o);
    }
    $('assetEditor').style.display = 'block';
    showAssetFile();
  } catch (e) {
    $('assetDetailCard').style.display = 'block';
    $('assetTitle').textContent = name;
    $('assetDetail').innerHTML = err('读取内容失败：' + e.message);
    $('assetEditor').style.display = 'none';
  }
}

// showAssetFile 把选中的文件内容填进编辑框。
function showAssetFile() {
  if (!currentAsset) return;
  const path = $('assetFile').value;
  if (path === 'manifest.yaml') {
    // 响应里 manifest 是 []byte，序列化后是 base64，这里要解码成文本给人编辑。
    $('assetContent').value = decode(currentAsset.manifest);
    return;
  }
  const f = (currentAsset.files || []).find((x) => x.path === path);
  $('assetContent').value = f ? decode(f.content) : '';
}

// decode 把 base64 内容还原成文本。
//
// 协议里文件内容是 []byte，Go 会按 base64 编码成 JSON 字符串。
// 资产都是文本（markdown、json），按 UTF-8 解码是安全的。
function decode(b64) {
  if (!b64) return '';
  try {
    const bin = atob(b64);
    const bytes = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
    return new TextDecoder('utf-8').decode(bytes);
  } catch (e) {
    return '';
  }
}

// encode 把文本编成 base64，与服务端的 []byte 对齐。
function encode(text) {
  const bytes = new TextEncoder().encode(text || '');
  let bin = '';
  for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
  return btoa(bin);
}

// addAssetFile 给当前 bundle 增加一个文件。
//
// 只在前端把新路径塞进下拉并切换过去，真正落盘要等“保存并下发”：
// 提前写入会让一个半成品文件立刻下发到所有订阅设备。
function addAssetFile() {
  confirmDo('新增文件', '新增的文件在点“保存并下发”之前不会写到云端。' +
    '如果是 skill，放一个 demo/SKILL.md 就会让服务端把 demo 识别成 skill 条目。', async () => {
    const p = ($('newFilePath').value || '').trim();
    if (!p) throw new Error('路径不能为空');
    if ((currentAsset.files || []).some((f) => f.path === p)) throw new Error('这个路径已经存在');
    const sel = $('assetFile');
    const o = document.createElement('option');
    o.value = p;
    o.textContent = p + '（新）';
    sel.appendChild(o);
    sel.value = p;
    $('assetContent').value = '';
    closeConfirm();
  }, {
    okText: '添加',
    extraHTML: '<label for="newFilePath">文件路径</label>' +
      '<input id="newFilePath" placeholder="例如 demo/SKILL.md">'
  });
}

// saveAssetFile 把整包写回。
//
// 写入是整体替换语义，所以哪怕只改了一个文件，也要把所有文件一起提交：
// 漏掉的文件会被当成已删除。
function saveAssetFile() {
  if (!currentAsset || !currentBundle) return;
  const path = $('assetFile').value;
  const text = $('assetContent').value;
  const msg = ($('assetMessage').value || '').trim();

  const files = [];
  for (const f of (currentAsset.files || [])) {
    files.push({
      path: f.path,
      content: f.path === path ? encode(text) : f.content,
      executable: !!f.executable
    });
  }
  // 原来没有这个文件，说明是新增：把编辑框里的内容补进去。
  if (!files.some((f) => f.path === path) && path !== 'manifest.yaml') {
    files.push({ path: path, content: encode(text) });
  }

  // manifest 原样写回：响应里它就是 base64 的原文，只有用户真的改了才重新编码。
  // 不原样带回去，服务端会按文件树重新推断，planes 与 host 这些声明会被悄悄改掉。
  let manifestRaw = currentAsset.manifest || '';
  if (path === 'manifest.yaml') {
    manifestRaw = text ? encode(text) : '';
  }

  confirmDo('写入资产', '把 ' + currentBundle + ' / ' + path + ' 的内容写回云端？' +
    '写入会立刻下发到订阅了它的设备，并留下一条可回滚的提交。', async () => {
    await api('write_asset', {
      bundle: currentBundle,
      items: files,
      manifest: manifestRaw || null,
      message: msg || ('管理面修改 ' + path)
    });
    $('assetMessage').value = '';
    await openBundle(currentBundle);
    await loadBundles();
  }, { okText: '写入并下发' });
}

function cancelAssetEdit() {
  $('assetEditor').style.display = 'none';
  $('assetDetailCard').style.display = 'none';
  currentAsset = null;
  currentBundle = null;
}

// ---------- 密钥 ----------

async function loadSecrets() {
  try {
    const d = await api('list_secrets');
    const list = d.secrets || [];
    if (!list.length) {
      $('secrets').innerHTML = empty('还没有任何密钥。');
      return;
    }
    let html = '<table><thead><tr><th>名称</th><th>说明</th><th>引用</th><th>更新</th><th></th></tr></thead><tbody>';
    for (const s of list) {
      const refs = (s.refs || []).map((r) => r.bundle + '/' + r.item).join('、');
      html += '<tr><td class="mono">' + esc(s.name) + '</td><td>' + esc(s.notes || '—') +
        '</td><td class="mono">' + esc(refs || '—') + '</td><td>' + esc(when(s.updated_at)) +
        '</td><td><button class="act" onclick="revealSecret(&quot;' + esc(s.name) +
        '&quot;)">查看</button> <button class="act" onclick="editSecret(&quot;' + esc(s.name) +
        '&quot;,&quot;' + esc(s.notes || '') + '&quot;)">改值</button> ' +
        '<button class="danger" onclick="removeSecret(&quot;' + esc(s.name) +
        '&quot;)">删除</button></td></tr>';
    }
    html += '</tbody></table>';
    $('secrets').innerHTML = html;
  } catch (e) {
    $('secrets').innerHTML = err('读取密钥失败：' + e.message);
  }
}

// revealSecret 显式查看明文。
//
// 必须填理由：这条路径会记进审计，理由就是事后唯一能解释“为什么看”的东西。
// 明文不写回列表——列表可能被截图或被投屏——而是单独出现，并带一个显眼的关闭入口。
function revealSecret(name) {
  confirmDo('查看密钥明文', '读取 ' + name + ' 的明文会记入审计日志。', async () => {
    const reason = ($('cfReason').value || '').trim();
    if (!reason) throw new Error('必须填写查看理由');
    const r = await api('read_secret', { name: name, reason: reason });
    showPlaintext(name, r.value, reason);
    closeConfirm();
  }, {
    okText: '查看',
    extraHTML: '<label for="cfReason">查看理由（记入审计）</label>' +
      '<input id="cfReason" placeholder="例如：需要核对上游服务是否被改过">'
  });
}

// showPlaintext 把明文放进一个临时卡片，并允许手动收起。
function showPlaintext(name, value, reason) {
  const old = $('plainCard');
  if (old) old.remove();
  const card = document.createElement('div');
  card.className = 'card';
  card.id = 'plainCard';
  card.innerHTML = '<h3>' + esc(name) + ' 的明文</h3>' +
    '<div class="row"><span class="k">值</span><span class="v">' + esc(value) + '</span></div>' +
    '<div class="row"><span class="k">理由</span><span class="v">' + esc(reason) + '</span></div>' +
    '<div class="toolbar" style="margin-top:12px">' +
    '<button class="act" onclick="document.getElementById(&quot;plainCard&quot;).remove()">收起</button>' +
    '</div>';
  $('page-secrets').appendChild(card);
}

function editSecret(name, notes) {
  confirmDo('修改密钥', '为 ' + name + ' 写入新值。写入后订阅设备会立刻收到新内容。', async () => {
    const value = $('cfValue').value;
    if (!value) throw new Error('新值不能为空');
    await api('put_secret', {
      name: name,
      value: value,
      notes: notes,
      message: ($('cfMsg').value || '').trim()
    });
    await loadSecrets();
  }, {
    okText: '写入并下发',
    extraHTML: '<label for="cfValue">新值</label><textarea id="cfValue"></textarea>' +
      '<label for="cfMsg">提交说明</label><input id="cfMsg" placeholder="例如：上游轮换了 token">'
  });
}

function openSecretForm() {
  confirmDo('新增密钥', '明文只存在于这一次请求里，写入后立刻加密。', async () => {
    const name = ($('cfName').value || '').trim();
    const value = $('cfValue').value;
    if (!name) throw new Error('名称不能为空');
    if (!value) throw new Error('值不能为空');
    await api('put_secret', {
      name: name,
      value: value,
      notes: ($('cfNotes').value || '').trim()
    });
    await loadSecrets();
  }, {
    okText: '写入',
    extraHTML: '<label for="cfName">名称</label><input id="cfName" placeholder="例如 github-token">' +
      '<label for="cfValue">值</label><textarea id="cfValue"></textarea>' +
      '<label for="cfNotes">说明（给人看，不含明文）</label><input id="cfNotes">'
  });
}

function removeSecret(name) {
  confirmDo('删除密钥', '删除 ' + name + '。如果还有资产引用它，删除会被拒绝，需要先改资产。',
    async () => {
      await api('delete_secret', { name: name, message: '管理面删除' });
      await loadSecrets();
    }, { danger: true, okText: '删除' });
}

// ---------- 历史 ----------

async function loadHistory() {
  const kind = $('histKind').value;
  const id = $('histID').value.trim();
  if (!id) {
    $('history').innerHTML = empty('填入 bundle 名或密钥名后显示历史。');
    return;
  }
  try {
    const d = await api('history', { kind: kind, id: id, limit: 50 });
    const list = d.entries || [];
    if (!list.length) {
      $('history').innerHTML = empty('没有历史记录。');
      return;
    }
    let html = '<table><thead><tr><th>时间</th><th>提交</th><th>说明</th><th>来源</th><th></th></tr></thead><tbody>';
    for (const e of list) {
      html += '<tr><td>' + esc(when(e.created_at)) + '</td><td class="mono">' +
        esc((e.commit || '').slice(0, 8)) + '</td><td>' + esc(e.message) + '</td><td>' +
        esc(e.source || e.author) + '</td><td><button class="act" onclick="rollback(&quot;' +
        esc(e.commit) + '&quot;,&quot;' + esc(e.message || '') + '&quot;)">回滚到此</button>' +
        '</td></tr>';
    }
    html += '</tbody></table>';
    $('history').innerHTML = html;
  } catch (e) {
    $('history').innerHTML = err('读取历史失败：' + e.message);
  }
}

function rollback(commit, message) {
  const kind = $('histKind').value;
  const id = $('histID').value.trim();
  // 回滚不是删历史：它把目标内容写成一条新提交，所以回滚本身也能再被回滚。
  // 这句话必须出现在确认框里，否则用户会以为自己在销毁数据。
  confirmDo('回滚', '把 ' + kind + '「' + id + '」回滚到提交 ' + commit.slice(0, 8) +
    '（' + message + '）。回滚会以一条新提交写回目标内容，不会改写历史，' +
    '因此这次回滚本身也可以再被回滚。执行后立刻下发。', async () => {
    await api('rollback', { kind: kind, id: id, commit: commit, message: '管理面回滚' });
    await loadHistory();
  }, { danger: true, okText: '回滚' });
}

// ---------- 项目 ----------

async function loadProjects() {
  // 本机登记：决定项目平面资产的落点，只能在本机改。
  try {
    const r = await fetch('/api/projects', { headers: { 'Accept': 'application/json' } });
    const d = await r.json();
    const list = d.projects || [];
    if (!list.length) {
      $('localProjects').innerHTML = empty('还没有登记项目。');
    } else {
      let html = '<table><thead><tr><th>ID</th><th>路径</th><th></th></tr></thead><tbody>';
      for (const p of list) {
        html += '<tr><td class="mono">' + esc(p.ID) + '</td><td class="mono">' + esc(p.Path) +
          '</td><td><button class="danger" onclick="removeLocalProject(&quot;' + esc(p.ID) +
          '&quot;)">移除</button></td></tr>';
      }
      html += '</tbody></table>';
      $('localProjects').innerHTML = html;
    }
    window.localProjects = list;
  } catch (e) {
    $('localProjects').innerHTML = err('读取本机项目失败：' + e.message);
  }

  // 云端登记与订阅：决定哪个项目订阅哪些 bundle。
  try {
    const d = await api('projects');
    const list = d.projects || [];
    if (!list.length) {
      $('cloudProjects').innerHTML = empty('还没有云端项目登记。');
    } else {
      let html = '<table><thead><tr><th>项目</th><th>设备</th><th>路径</th><th>订阅</th><th></th></tr></thead><tbody>';
      for (const p of list) {
        html += '<tr><td>' + esc(p.name) + '</td><td>' + esc(p.device_name || p.device_id) +
          '</td><td class="mono">' + esc(p.path) + '</td><td class="mono">' +
          esc((p.bundles || []).join('、') || '—') + '</td><td>' +
          '<button class="danger" onclick="removeCloudProject(&quot;' + esc(p.id) + '&quot;,&quot;' +
          esc(p.name) + '&quot;)">删除</button></td></tr>';
      }
      html += '</tbody></table>';
      $('cloudProjects').innerHTML = html;
    }
  } catch (e) {
    $('cloudProjects').innerHTML = err('读取云端项目失败：' + e.message);
  }

  await loadSubscriptionDevices();
}

// loadSubscriptionDevices 填设备下拉，并显示选中设备的订阅。
async function loadSubscriptionDevices() {
  try {
    const d = await api('devices');
    const list = (d.devices || []).filter((x) => !x.revoked);
    const sel = $('subDevice');
    const keep = sel.value;
    sel.innerHTML = '';
    for (const x of list) {
      const o = document.createElement('option');
      o.value = x.id;
      o.textContent = (x.name || '（未命名）') + ' · ' + x.id;
      sel.appendChild(o);
    }
    if (keep && list.some((x) => x.id === keep)) sel.value = keep;
    if (list.length) await loadSubscriptions();
    else $('subscriptions').innerHTML = empty('没有可用设备。');
  } catch (e) {
    $('subscriptions').innerHTML = err('读取设备失败：' + e.message);
  }
}

// loadSubscriptions 显示选中设备已订阅的 bundle，并允许调整。
async function loadSubscriptions() {
  const deviceID = $('subDevice').value;
  if (!deviceID) {
    $('subscriptions').innerHTML = empty('先选一台设备。');
    return;
  }
  try {
    const [dv, ab] = await Promise.all([api('devices'), api('list_assets')]);
    const dev = (dv.devices || []).find((x) => x.id === deviceID);
    if (!dev) {
      $('subscriptions').innerHTML = empty('设备已不存在。');
      return;
    }
    const has = new Set(dev.user_bundles || []);
    const all = ab.bundles || [];
    if (!all.length) {
      $('subscriptions').innerHTML = empty('还没有 bundle 可以订阅。');
      return;
    }
    let html = '<table><thead><tr><th>Bundle</th><th>条目数</th><th>订阅</th></tr></thead><tbody>';
    for (const b of all) {
      const on = has.has(b.name);
      html += '<tr><td class="mono">' + esc(b.name) + '</td><td>' + esc(b.items || 0) +
        '</td><td>' + (on ? '<span class="ok">已订阅</span>' : '<span class="tag">未订阅</span>') +
        '</td></tr>';
    }
    html += '</tbody></table>';
    $('subscriptions').innerHTML = html;
    window.subDevice = deviceID;
    window.subAll = all.map((b) => b.name);
    window.subHas = Array.from(has);
    $('subscriptions').insertAdjacentHTML('beforeend',
      '<div class="toolbar" style="margin-top:12px">' +
      '<button class="pri" onclick="editSubscriptions()">调整订阅</button></div>');
  } catch (e) {
    $('subscriptions').innerHTML = err('读取订阅失败：' + e.message);
  }
}

function editSubscriptions() {
  const deviceID = window.subDevice;
  const all = window.subAll || [];
  const has = new Set(window.subHas || []);
  if (!deviceID || !all.length) return;
  let extra = '<label>勾选这台设备要接收的 bundle</label>';
  for (const name of all) {
    extra += '<div class="row"><span class="k mono">' + esc(name) + '</span>' +
      '<input type="checkbox" id="sub-' + esc(name) + '"' + (has.has(name) ? ' checked' : '') +
      ' style="width:auto"></div>';
  }
  confirmDo('调整订阅', '取消订阅会把这台设备上对应的落点删掉。', async () => {
    const picked = all.filter((n) => $('sub-' + n).checked);
    await api('set_subscriptions', { device_id: deviceID, bundles: picked });
    await loadSubscriptions();
  }, { okText: '保存', extraHTML: extra });
}

function addLocalProject() {
  confirmDo('登记项目', '项目路径必须是这台机器上的绝对路径，且以后云端只能往这个目录下发。', async () => {
    const id = ($('cfID').value || '').trim();
    const path = ($('cfPath').value || '').trim();
    if (!id || !path) throw new Error('ID 与路径都必须填');
    const list = (window.localProjects || []).concat([{ ID: id, Path: path }]);
    await postJSON('/api/projects', list);
    await loadProjects();
  }, {
    okText: '登记',
    extraHTML: '<label for="cfID">项目 ID</label><input id="cfID" placeholder="例如 dec">' +
      '<label for="cfPath">本机绝对路径</label><input id="cfPath" placeholder="/Users/me/workspace/dec">'
  });
}

// addCloudProject 在云端登记一个项目，并指定它订阅哪些 bundle。
function addCloudProject() {
  confirmDo('登记云端项目', '项目登记告诉云端“这台机器的这个目录要接收哪些 bundle”。' +
    '路径必须与这台机器上的本机项目登记一致。', async () => {
    const name = ($('cfName').value || '').trim();
    const path = ($('cfPath').value || '').trim();
    const bundles = ($('cfBundles').value || '').split(',').map((s) => s.trim()).filter(Boolean);
    if (!name || !path) throw new Error('名称与路径都必须填');
    const deviceID = $('cfDevice').value;
    if (!deviceID) throw new Error('没有可用设备');
    await api('put_project', {
      device_id: deviceID, name: name, path: path, bundles: bundles
    });
    await loadProjects();
  }, {
    okText: '登记',
    extraHTML: '<label for="cfName">项目名</label><input id="cfName" placeholder="例如 dec">' +
      '<label for="cfPath">本机绝对路径</label><input id="cfPath" placeholder="/Users/me/workspace/dec">' +
      '<label for="cfBundles">订阅的 bundle（逗号分隔）</label>' +
      '<input id="cfBundles" placeholder="woa, tools">' +
      '<label for="cfDevice">所属设备</label>' +
      '<select id="cfDevice"></select>'
  });
  // 下拉要在对话框插进 DOM 之后才填得进去。
  fillDeviceSelect('cfDevice');
}

// fillDeviceSelect 用设备列表填一个下拉。
function fillDeviceSelect(id) {
  const sel = $(id);
  if (!sel) return;
  api('devices').then((d) => {
    sel.innerHTML = '';
    for (const x of (d.devices || [])) {
      if (x.revoked) continue;
      const o = document.createElement('option');
      o.value = x.id;
      o.textContent = (x.name || '（未命名）') + ' · ' + x.id;
      sel.appendChild(o);
    }
  }).catch(() => {});
}

function removeLocalProject(id) {
  confirmDo('移除项目登记', '移除后云端不能再往这个目录下发项目平面资产。已经落地的文件不会自动删除。',
    async () => {
      const list = (window.localProjects || []).filter((p) => p.ID !== id);
      await postJSON('/api/projects', list);
      await loadProjects();
    }, { danger: true, okText: '移除' });
}

function removeCloudProject(id, name) {
  confirmDo('删除项目登记', '删除云端对「' + name + '」的登记。这台机器上的文件不会自动删除。',
    async () => {
      await api('delete_project', { id: id });
      await loadProjects();
    }, { danger: true, okText: '删除' });
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

// ---------- 启动 ----------

document.querySelectorAll('#nav button').forEach((b) => {
  b.addEventListener('click', () => show(b.dataset.page));
});
$('assetFile').addEventListener('change', showAssetFile);

show('overview');
setInterval(() => {
  if ($('page-overview').classList.contains('on')) loadOverview();
}, 10000);
`
