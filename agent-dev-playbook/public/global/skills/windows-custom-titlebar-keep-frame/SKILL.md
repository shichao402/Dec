---
name: windows-custom-titlebar-keep-frame
description: >
  Windows 桌面壳「页面自绘标题栏、保留原生窗框」的经验手册。覆盖为何不走
  decorations:false、如何用 WM_NCCALCSIZE 只砍标题栏、WebView2 顶边 resize
  热区、drag-region / 窗口按钮、以及远程页窄能力面。新产品要做无系统标题栏
  的 Tauri / Win32 WebView 壳时使用。
---

# Windows：自绘标题栏，保留原生窗框

目标外观是「没有系统标题栏，页面顶栏就是标题栏」，但左 / 右 / 下边缘缩放、
Aero Snap、DWM 阴影、Win11 圆角仍由系统完成。

这不是真正的「无边框」（`decorations: false` / 去掉整个 frame）。
无边框等于把缩放、贴边、阴影、圆角全部改成自模拟；相关踩坑见
tauri#8519、tauri#13134。

## 一、先选对路径

| 需求 | 做法 | 不要做 |
|---|---|---|
| 主窗口：无系统标题栏，但仍要原生缩放 / Snap / 阴影 | 保留 `decorations` 默认开启；子类化截 `WM_NCCALCSIZE`，把客户区顶边抬回窗口顶 | 主窗 `decorations: false` |
| 短暂 splash / 不进任务栏的提示窗 | 可以 `decorations: false` + `skipTaskbar` | 把主窗也做成 splash 那一套 |
| 完全自绘圆角、阴影、边缘命中 | 真无边框 + 自己模拟 | 与上表主窗路径混用，却指望系统 Snap 仍正常 |

主窗窗口样式一个都不改，任务栏项、`Alt+Space` 系统菜单、最大化行为才能照旧。

## 二、原生侧：只砍标题栏

1. 对主窗 HWND 挂一次 `SetWindowSubclass`（同一窗口只挂一次，重复挂会叠两层默认处理）。
2. 在 `WM_NCCALCSIZE`（`wparam != 0`）里：先 `DefSubclassProc`，再把
   `NCCALCSIZE_PARAMS.rgrc[0].top` 抬回「请求的顶边」。
3. **最大化例外**：最大化时窗口比工作区大出一圈边框；顶边若抬到窗口顶，
   内容会顶出屏幕。最大化时顶边要保留
   `SM_CYSIZEFRAME + SM_CXPADDEDBORDER`（按窗口 DPI 取）。
4. 挂完 subclass 后立刻 `SetWindowPos(..., SWP_FRAMECHANGED | SWP_NOMOVE |
   SWP_NOSIZE | …)`。非客户区尺寸是缓存的，不主动要一次 frame 变更，
   标题栏会留到第一次 resize 才消失。
5. 可选：在 `WM_NCHITTEST` 里，非最大化且命中客户区顶边一圈时回报 `HTTOP`。
   WebView2 子窗口盖住的部分收不到这条；盖不住的几像素仍可借它拖。

失败只应表现为「系统标题栏还在」，不要因此拦住启动。

伪代码骨架：

```text
WM_NCCALCSIZE (wparam != 0):
  requested_top = params.rgrc[0].top
  result = DefSubclassProc(...)
  keep = requested_top + (IsZoomed ? top_frame_dpi : 0)
  if params.rgrc[0].top > keep:
    params.rgrc[0].top = keep
  return result
```

## 三、页面侧：顶栏兼任标题栏

系统标题栏没了之后，页面必须自己承担：

1. **拖拽移动 / 双击最大化**  
   顶栏空白区域挂 `data-tauri-drag-region`（或宿主等价 drag-region）。
   按钮、输入框不要放在 drag-region 里，否则点不到。

2. **最小化 / 最大化 / 关闭**  
   右上角自绘窗口按钮，调用宿主窗口 API
   （Tauri：`minimize` / `toggleMaximize` / `close` / `isMaximized`）。
   最大化状态变化时同步按钮字形（还原 vs 最大化），并给 `body` 加
   `is-maximized` 一类标记，供 CSS 隐藏顶边 resize。

3. **顶边与两角缩放**  
   WebView2 盖住非客户区后，顶边原生 `HTTOP` 大多失效。
   在窗口最顶铺约 **5px** 透明热区（左右角各约 7px），
   `pointerdown` 时调用 `startResizeDragging("North" | "NorthWest" | "NorthEast")`，
   最终仍进入 Windows 系统 resize loop，而不是自己画框拖。
   **最大化时热区必须隐藏**，避免误触。

4. **布局留白**  
   顶栏高度与窗口按钮宽度用 CSS 变量统一；内容区不要被按钮挡住。
   横幅 / toast 不要压在第一行右上角（窗口按钮钉在那里）。

## 四、能力面要收窄

若 WebView 加载的是 **loopback / 远程页**（不是壳本地静态页）：

1. 页面只允许一个窄适配层碰窗口装饰 IPC（关闭、最小化、最大化、读最大化、
   顶边 resize、drag-region 所需命令）。
2. **不要**导出通用 `invoke`，不要把业务切面改成壳 IPC。
3. Capability / ACL 只放装饰相关权限，例如 Tauri：
   - `core:window:allow-close`
   - `core:window:allow-minimize`
   - `core:window:allow-toggle-maximize` / `allow-internal-toggle-maximize`
   - `core:window:allow-is-maximized`
   - `core:window:allow-start-dragging`
   - `core:window:allow-start-resize-dragging`
4. 浏览器或无壳注入时，装饰动作应安全退化为空操作，便于同一套前端在
   Playwright / 系统浏览器里跑。

壳本地页（启动器 UI）可以直接用 `getCurrentWindow()`；远程业务页走窄适配层。

## 五、splash 与主窗不要混策略

| | 主窗 | splash |
|---|---|---|
| 标题栏 | 页面自绘 + `WM_NCCALCSIZE` | 通常无系统栏 |
| decorations | 保持开启 | 常 `false` |
| 任务栏 | 有 | 常 `skipTaskbar` |
| 退出口 | 窗口关闭按钮 / 系统菜单 | **必须自带退出**（否则人只剩任务管理器） |
| 生命周期 | 产品主面 | 画完即显、主窗就绪后关掉 |

## 六、验收清单

做完后逐项点验：

- [ ] 主窗无系统标题栏，页面顶栏可拖动、可双击最大化
- [ ] 左 / 右 / 下边缘原生缩放仍在；顶边与两角经热区缩放
- [ ] Aero Snap（拖到边缘分屏）仍在
- [ ] DWM 阴影与 Win11 圆角仍在
- [ ] 最大化后内容不顶出屏幕；顶边 resize 热区消失
- [ ] 还原后热区恢复；最大化按钮字形正确
- [ ] 任务栏右键 / `Alt+Space` 系统菜单仍可用
- [ ] 远程页无法 `invoke` 业务命令，只能做窗口装饰
- [ ] 无壳环境下前端不因缺 `__TAURI__` 崩溃
- [ ] splash（若有）可退出，不会在主窗出现后残留挡操作

## 七、失败模式（表象 → 原因）

| 表象 | 常见原因 |
|---|---|
| 缩放 / Snap / 阴影全丢，还要自己画框 | 主窗走了 `decorations: false` |
| 标题栏砍了，但第一次 resize 才消失 | 挂 subclass 后没 `SWP_FRAMECHANGED` |
| 最大化后顶部内容被裁切 | `WM_NCCALCSIZE` 没保留最大化顶边框厚度 |
| 顶边拖不动缩放，左右下正常 | WebView2 吃掉顶边命中，缺 `startResizeDragging` 热区 |
| 最大化后顶边仍能误触发缩放 | 热区未在 `is-maximized` 时隐藏 |
| 顶栏按钮点不到 | 按钮落在 `data-tauri-drag-region` 内 |
| 远程页永远调不了窗口 API | capability 未放给 loopback URL / 缺 `allow-start-*` |
| 业务页能随便 `invoke` 壳命令 | 未做窄适配层，能力面过大 |
| splash 阶段无法退出 | splash 无关闭入口且 `skipTaskbar` |

## 八、适用范围

- **适用**：Windows 上 Tauri 2 / Win32 + WebView2 的桌面产品主窗。
- **可借鉴**：任何「只要自绘标题栏、不要重写整个 frame」的 Win32 宿主。
- **不适用**：macOS traffic lights / titleBarStyle（另一套契约）；真无边框游戏全屏；
  非 Windows 的边缘命中模型。
