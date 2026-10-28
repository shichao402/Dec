package agent

// 管理面的页面外壳（需求 14.1、14.2）。
//
// 需求明确要求管理面不得在公网暴露 Web 页面：浏览器访问的是代理在
// 127.0.0.1 上开的这个页面，所有请求由代理带着设备证书转发给云端。
// 因此页面只能是代理二进制里的一段静态资源，不能放到服务端。
//
// 页面刻意做成单页、无构建、无外链：代理是下载即跑的单文件程序，
// 页面必须能在断网时打开（离线时显示状态，不允许操作），也不能依赖 CDN。
const adminHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Dec 管理面</title>
<style>
:root { color-scheme: light dark; --bg:#f6f7f9; --card:#fff; --line:#e5e7eb; --txt:#1c1f23; --mut:#6b7280; --ok:#0f7b3f; --bad:#b42318; --warn:#b54708; }
@media (prefers-color-scheme: dark) {
  :root { --bg:#16181c; --card:#1e2126; --line:#2f333a; --txt:#e6e8eb; --mut:#9aa1ab; --ok:#4ade80; --bad:#f87171; --warn:#fbbf24; }
}
* { box-sizing: border-box; }
body { margin:0; font:14px/1.6 -apple-system, "Segoe UI", system-ui, sans-serif; background:var(--bg); color:var(--txt); }
header { border-bottom:1px solid var(--line); background:var(--card); position:sticky; top:0; z-index:10; }
.wrap { max-width:1040px; margin:0 auto; padding:0 20px; }
.bar { display:flex; align-items:center; gap:20px; height:56px; }
.logo { font-weight:600; letter-spacing:.01em; }
nav { display:flex; gap:2px; flex:1; }
nav button { background:transparent; border:0; color:var(--mut); font:inherit; padding:6px 12px; border-radius:7px; cursor:pointer; }
nav button:hover { background:var(--bg); color:var(--txt); }
nav button.on { background:var(--bg); color:var(--txt); font-weight:500; }
main { max-width:1040px; margin:0 auto; padding:24px 20px 80px; }
section { display:none; }
section.on { display:block; }
h2 { font-size:16px; margin:0 0 4px; }
.sub { color:var(--mut); font-size:13px; margin:0 0 18px; }
.card { background:var(--card); border:1px solid var(--line); border-radius:10px; padding:16px 18px; margin-bottom:14px; }
.card h3 { font-size:13px; margin:0 0 12px; color:var(--mut); font-weight:500; letter-spacing:.02em; }
.row { display:flex; justify-content:space-between; gap:16px; padding:7px 0; border-bottom:1px dashed var(--line); align-items:center; }
.row:last-child { border-bottom:0; }
.k { color:var(--mut); flex:0 0 auto; }
.v { font-family:ui-monospace, SFMono-Regular, Menlo, monospace; text-align:right; word-break:break-all; }
.ok { color:var(--ok); } .bad { color:var(--bad); } .warn { color:var(--warn); }
table { width:100%; border-collapse:collapse; font-size:13px; }
th { text-align:left; color:var(--mut); font-weight:500; padding:6px 8px; border-bottom:1px solid var(--line); white-space:nowrap; }
td { padding:7px 8px; border-bottom:1px solid var(--line); vertical-align:top; overflow-wrap:anywhere; }
td.nowrap { white-space:nowrap; }
td.paths { word-break:break-all; }
.pathline { display:block; }
tr:last-child td { border-bottom:0; }
.mono { font-family:ui-monospace, SFMono-Regular, Menlo, monospace; font-size:12px; }
button.act { background:var(--bg); border:1px solid var(--line); color:var(--txt); font:inherit; font-size:12px; padding:4px 10px; border-radius:6px; cursor:pointer; }
button.act:hover { border-color:var(--mut); }
button.pri { background:#1c1f23; border:1px solid #1c1f23; color:#fff; font:inherit; padding:7px 16px; border-radius:7px; cursor:pointer; }
button.pri.hot { background:var(--bad); border-color:var(--bad); color:#fff; }
@media (prefers-color-scheme: dark) { button.pri { background:#e6e8eb; border-color:#e6e8eb; color:#16181c; } }
button.danger { background:transparent; border:1px solid var(--bad); color:var(--bad); font:inherit; font-size:12px; padding:4px 10px; border-radius:6px; cursor:pointer; }
label { display:block; font-size:12px; color:var(--mut); margin:10px 0 4px; }
input, textarea, select { width:100%; padding:7px 10px; border:1px solid var(--line); border-radius:7px; font:inherit; background:var(--card); color:var(--txt); }
textarea { min-height:120px; font-family:ui-monospace, SFMono-Regular, Menlo, monospace; font-size:12px; }
.toolbar { display:flex; gap:8px; align-items:center; margin-bottom:12px; flex-wrap:wrap; }
.tag { display:inline-block; font-size:11px; padding:1px 7px; border-radius:20px; background:var(--bg); border:1px solid var(--line); color:var(--mut); }
.tag.ok { color:var(--ok); border-color:currentColor; }
.tag.bad { color:var(--bad); border-color:currentColor; }
.tag.warn { color:var(--warn); border-color:currentColor; }
.mask { font-family:ui-monospace, monospace; color:var(--mut); letter-spacing:.15em; }
.empty { color:var(--mut); padding:18px 0; text-align:center; font-size:13px; }
.err { color:var(--bad); font-size:13px; }
dialog { border:1px solid var(--line); border-radius:12px; padding:0; max-width:560px; width:calc(100% - 40px); background:var(--card); color:var(--txt); }
dialog::backdrop { background:rgba(0,0,0,.4); }
.dlg-body { padding:20px 22px; }
.dlg-foot { display:flex; justify-content:flex-end; gap:8px; padding:14px 22px; border-top:1px solid var(--line); }
.banner { padding:10px 14px; border-radius:8px; font-size:13px; margin-bottom:14px; border:1px solid var(--line); background:var(--card); }
.banner.bad { border-color:var(--bad); color:var(--bad); }
</style>
</head>
<body>
<header>
  <div class="wrap bar">
    <span class="logo">Dec</span>
    <nav id="nav">
      <button data-page="overview" class="on">概览</button>
      <button data-page="devices">设备</button>
      <button data-page="assets">资产</button>
      <button data-page="secrets">密钥</button>
      <button data-page="history">历史</button>
      <button data-page="projects">项目</button>
      <button data-page="tools">工具</button>
    </nav>
    <span id="conn" class="tag" title="正在读取本机状态">检查中</span>
  </div>
</header>
<main>
  <div id="banner"></div>

  <section id="page-overview" class="on">
    <h2>概览</h2>
    <p class="sub">内容以云端为准，这台机器只负责落盘与转发。</p>
    <div id="legacyHint"></div>
    <div class="card"><h3>本地代理</h3><div id="local"></div></div>
    <div class="card"><h3>云端服务</h3><div id="cloud"></div></div>
  </section>

  <section id="page-devices">
    <h2>设备</h2>
    <p class="sub">设备身份是客户端证书，吊销立即生效，不依赖设备在线。版本未对齐的设备不会收到下发。</p>
    <div class="card"><div id="devices"></div></div>
    <div class="card"><h3>吊销后的轮换清单</h3><div id="rotation"></div></div>
    <div class="card"><h3>本地改动覆盖记录</h3><div id="overrides"></div></div>
  </section>

  <section id="page-assets">
    <h2>资产</h2>
    <p class="sub">写入即下发。资产里的密钥是占位符，明文由云端在渲染时内联。</p>
    <div class="card"><h3>Bundles</h3>
      <div class="toolbar"><button class="pri" onclick="openNewBundle()">新建 bundle</button></div>
      <div id="bundles"></div>
    </div>
    <div class="card" id="assetDetailCard" style="display:none">
      <h3 id="assetTitle">内容</h3>
      <div id="assetDetail"></div>
      <div id="assetEditor" style="display:none">
        <label for="assetFile">编辑文件</label>
        <div class="toolbar" style="margin:0 0 4px">
          <select id="assetFile" style="flex:1"></select>
          <button class="act" onclick="addAssetFile()">新增文件</button>
        </div>
        <label for="assetContent">内容</label>
        <textarea id="assetContent"></textarea>
        <label for="assetMessage">提交说明</label>
        <input id="assetMessage" placeholder="例如：修正常量命名">
        <div class="toolbar" style="margin-top:12px">
          <button class="pri" onclick="saveAssetFile()">保存并下发</button>
          <button class="act" onclick="cancelAssetEdit()">取消</button>
        </div>
      </div>
    </div>
  </section>

  <section id="page-secrets">
    <h2>密钥</h2>
    <p class="sub">默认遮蔽。显式查看明文会记入审计日志。</p>
    <div class="card">
      <div class="toolbar">
        <button class="pri" onclick="openSecretForm()">新增密钥</button>
      </div>
      <div id="secrets"></div>
    </div>
  </section>

  <section id="page-history">
    <h2>历史与回滚</h2>
    <p class="sub">回滚不会改写历史，而是把目标内容以一个新提交写回，因此回滚本身也能再被回滚。</p>
    <div class="card">
      <div class="toolbar">
        <select id="histKind" style="width:auto" onchange="loadHistory()">
          <option value="asset">资产</option>
          <option value="secret">密钥</option>
        </select>
        <input id="histID" placeholder="bundle 或密钥名" style="width:220px" onchange="loadHistory()">
      </div>
      <div id="history"></div>
    </div>
  </section>

  <section id="page-projects">
    <h2>项目</h2>
    <p class="sub">项目登记决定项目平面资产落在哪个目录。路径必须在本机已登记。</p>
    <div class="card">
      <h3>本机项目登记</h3>
      <div class="toolbar"><button class="pri" onclick="addLocalProject()">登记项目</button></div>
      <div id="localProjects"></div>
    </div>
    <div class="card">
      <h3>云端项目与订阅</h3>
      <div class="toolbar"><button class="pri" onclick="addCloudProject()">登记云端项目</button></div>
      <div id="cloudProjects"></div>
    </div>
    <div class="card">
      <h3>用户平面订阅</h3>
      <p class="sub" style="margin:0 0 12px">决定每台设备拿到哪些 bundle。只接收设备默认为空。</p>
      <div class="toolbar">
        <select id="subDevice" style="width:auto" onchange="loadSubscriptions()"></select>
      </div>
      <div id="subscriptions"></div>
    </div>
  </section>

  <section id="page-tools">
    <h2>工具</h2>
    <p class="sub">一次性的本机运维动作。只在这台机器上执行，不经云端。</p>
    <div class="card" id="tool-legacy">
      <h3>旧版残留清理</h3>
      <p class="sub" style="margin:0 0 12px">找出旧版 Dec 留在这台机器和已登记项目里的文件与配置条目。删除前整体压缩归档，保留 365 天，可整份或单项还原。项目里被 git 跟踪的文件删掉后会自动提交，能快进时推送。</p>
      <div class="toolbar"><button class="pri" onclick="scanLegacy()">扫描</button><span id="legacyScanAt" class="sub" style="margin:0"></span></div>
      <div id="legacyScan"></div>
      <div id="legacyResult"></div>
    </div>
    <div class="card" id="tool-legacy-archives">
      <h3>清理归档</h3>
      <div id="legacyArchives"></div>
      <div id="legacyArchive"></div>
    </div>
  </section>
</main>

<dialog id="confirmDlg">
  <div class="dlg-body">
    <h3 id="cfTitle" style="margin:0 0 8px;font-size:15px"></h3>
    <div id="cfBody" style="color:var(--mut);font-size:13px"></div>
    <div id="cfExtra" style="margin-top:12px"></div>
    <div id="cfError" style="margin-top:12px"></div>
  </div>
  <div class="dlg-foot">
    <button class="act" onclick="closeConfirm()">取消</button>
    <button class="pri" id="cfOK">确认</button>
  </div>
</dialog>
<script src="/admin.js"></script>
</body>
</html>
`
