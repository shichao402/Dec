export type Severity = 'error' | 'warn'

export type Finding = {
  rule: string
  severity: Severity
  target: string
  detail: string
}

export type ProbeOptions = {
  // PageFill 页底部允许的空白：超过就说明列表没有撑满视口。
  deadSpaceRatio: number
  // 内容横向占用下限：低于就说明宽屏留了大片空白。
  widthUsageRatio: number
  // 内容列占可用宽度的下限：低于就说明 max-width 卡得太窄。
  containerUsageRatio: number
  // 嵌套滚动容器占外层滚动区高度的上限。
  nestedScrollShare: number
  // 交互元素最小可点面积。
  minTargetSize: number
}

export type ProbeReport = {
  findings: Finding[]
  metrics: {
    viewport: { width: number; height: number }
    pageKind: string
    contentBottom: number
    deadSpace: number
    widthUsage: number
    scrollContainers: number
  }
}

// 这个函数会被序列化进浏览器执行，因此不能引用模块作用域之外的任何东西。
export function probeLayout(options: ProbeOptions): ProbeReport {
  const findings: Finding[] = []
  const viewport = { width: window.innerWidth, height: window.innerHeight }

  const describe = (el: Element): string => {
    const parts: string[] = []
    let node: Element | null = el
    for (let depth = 0; node && depth < 4; depth += 1) {
      let part = node.tagName.toLowerCase()
      const testId = node.getAttribute('data-testid')
      if (testId) part += `[${testId}]`
      else if (node.id) part += `#${node.id}`
      parts.unshift(part)
      node = node.parentElement
    }
    const text = (el.textContent || '').replace(/\s+/g, ' ').trim().slice(0, 40)
    return text ? `${parts.join('>')} “${text}”` : parts.join('>')
  }

  const visible = (el: Element): boolean => {
    const style = getComputedStyle(el)
    if (style.display === 'none' || style.visibility === 'hidden') return false
    if (Number(style.opacity) === 0) return false
    const rect = el.getBoundingClientRect()
    return rect.width >= 1 && rect.height >= 1
  }

  const scrollable = (el: Element, axis: 'x' | 'y'): boolean => {
    const style = getComputedStyle(el)
    const overflow = axis === 'y' ? style.overflowY : style.overflowX
    if (overflow !== 'auto' && overflow !== 'scroll') return false
    return axis === 'y'
      ? el.scrollHeight > el.clientHeight + 1
      : el.scrollWidth > el.clientWidth + 1
  }

  const clips = (el: Element, axis: 'x' | 'y'): boolean => {
    const style = getComputedStyle(el)
    const overflow = axis === 'y' ? style.overflowY : style.overflowX
    return overflow === 'auto' || overflow === 'scroll' || overflow === 'hidden' || overflow === 'clip'
  }

  const page = document.querySelector('[data-page]')
  const container = document.querySelector('[data-page-container]') || page
  const pageKind = page?.getAttribute('data-page') || 'none'
  // 内容边界只在页面区域内测（侧栏底部的文字会掩盖主内容的底部留白），
  // 溢出、嵌套滚动、遮挡这些结构性检查则覆盖整个文档。
  const scope: Element = page || document.body
  const all: Element = document.body

  // ---- 画出来的内容边界 ----------------------------------------------------
  // 文字和图标决定「视觉上内容到哪里结束」；边框和底色决定「面板铺到哪里」。
  // 两者分开测：面板可以撑满而文字很短，这正是「布局没错但看着空」的情形。
  const textRects: DOMRect[] = []
  const walker = document.createTreeWalker(scope, NodeFilter.SHOW_TEXT)
  while (walker.nextNode()) {
    const node = walker.currentNode
    if (!node.nodeValue || !node.nodeValue.trim()) continue
    const parent = node.parentElement
    if (!parent || !visible(parent)) continue
    const range = document.createRange()
    range.selectNodeContents(node)
    for (const rect of Array.from(range.getClientRects())) {
      if (rect.width >= 1 && rect.height >= 1) textRects.push(rect)
    }
  }
  for (const el of Array.from(scope.querySelectorAll('svg, img'))) {
    if (visible(el)) textRects.push(el.getBoundingClientRect())
  }

  const boxRects: DOMRect[] = []
  for (const el of Array.from(scope.querySelectorAll('*'))) {
    if (!visible(el)) continue
    const style = getComputedStyle(el)
    const hasBorder =
      style.borderStyle !== 'none' &&
      (parseFloat(style.borderTopWidth) > 0 ||
        parseFloat(style.borderBottomWidth) > 0 ||
        parseFloat(style.borderLeftWidth) > 0 ||
        parseFloat(style.borderRightWidth) > 0)
    const bg = style.backgroundColor
    const hasBg = Boolean(bg) && bg !== 'transparent' && !bg.startsWith('rgba(0, 0, 0, 0')
    if (hasBorder || hasBg) boxRects.push(el.getBoundingClientRect())
  }

  const painted = [...textRects, ...boxRects]
  const contentBottom = textRects.reduce((max, rect) => Math.max(max, rect.bottom), 0)
  const paintedLeft = painted.reduce((min, rect) => Math.min(min, rect.left), viewport.width)
  const paintedRight = painted.reduce((max, rect) => Math.max(max, rect.right), 0)

  // ---- 竖向死空间 ---------------------------------------------------------
  // 「下面空着」本身不是错：内容短的时候留白是正常的，业界都这么做。
  // 真正的错是「一边把内容截断滚动，一边下面空着」——那说明高度没分配对。
  const pageRect = (page || document.body).getBoundingClientRect()
  const deadSpace = Math.max(0, pageRect.bottom - contentBottom)
  const budget = Math.max(72, viewport.height * options.deadSpaceRatio)
  const clipping = Array.from((scope as Element).querySelectorAll('*')).some(
    (el) => visible(el) && scrollable(el, 'y'),
  )
  // 内容短、面板贴合内容、下方留页面底色，是 GitHub / Vercel 这类控制台的常态，不算问题；
  // 所以纯粹的空白只记进 metrics，只有「一边截断一边空着」才判错。
  if (clipping && deadSpace > budget) {
    findings.push({
      rule: 'dead-space',
      severity: 'error',
      target: describe(page || document.body),
      detail: `底部空出 ${Math.round(deadSpace)}px（阈值 ${Math.round(budget)}px），同时页面内还有容器在滚动。高度没有分配给需要它的列表。`,
    })
  }

  // ---- 横向利用率 ---------------------------------------------------------
  const containerRect = (container || document.body).getBoundingClientRect()
  const widthUsage = containerRect.width > 0 ? (paintedRight - paintedLeft) / containerRect.width : 1
  if (widthUsage < options.widthUsageRatio) {
    findings.push({
      rule: 'width-usage',
      severity: 'error',
      target: describe(container as Element),
      detail: `内容只用了容器宽度的 ${(widthUsage * 100).toFixed(0)}%（容器 ${Math.round(containerRect.width)}px），宽屏会留大片空白。`,
    })
  }
  // 容器自身被 max-width 卡得太窄时，上面那条查不出来：内容铺满了一个过窄的容器。
  if (page && container && container !== page) {
    const available = page.clientWidth
    const usage = available > 0 ? containerRect.width / available : 1
    if (usage < options.containerUsageRatio) {
      findings.push({
        rule: 'container-usage',
        severity: 'warn',
        target: describe(container),
        detail: `内容列 ${Math.round(containerRect.width)}px 只占可用宽度 ${available}px 的 ${(usage * 100).toFixed(0)}%。`,
      })
    }
  }

  // ---- 横向溢出 -----------------------------------------------------------
  for (const el of Array.from(all.querySelectorAll('*'))) {
    if (!visible(el)) continue
    // 省略号截断是有意为之，由 truncated-text 单独衡量，不算「被裁掉」。
    if (getComputedStyle(el).textOverflow === 'ellipsis') continue
    // 输入框天然可以装下比可视宽度更长的值，光标移动即可看到全部。
    const tag = el.tagName.toLowerCase()
    if (tag === 'input' || tag === 'textarea') continue
    if (el.scrollWidth > el.clientWidth + 1 && clips(el, 'x') && !scrollable(el, 'x')) {
      findings.push({
        rule: 'overflow-x',
        severity: 'error',
        target: describe(el),
        detail: `内容宽 ${el.scrollWidth}px 超过可视 ${el.clientWidth}px，且不可横向滚动，右侧被裁掉。`,
      })
    }
  }
  if (document.documentElement.scrollWidth > viewport.width + 1) {
    findings.push({
      rule: 'overflow-x',
      severity: 'error',
      target: 'document',
      detail: `文档宽 ${document.documentElement.scrollWidth}px 超过视口 ${viewport.width}px，出现整页横向滚动。`,
    })
  }

  // ---- 嵌套竖向滚动 -------------------------------------------------------
  // 「长列表被外层整页滚动切断」的根因就是同一条祖先链上出现两个滚动容器。
  // 例外：有明确高度上限的小区域（目录选择器之类）嵌在长页面里是通行做法，
  // 因为它自带视觉边界。真正会让人迷惑的是两个都想吃满剩余高度的滚动区。
  const scrollers = Array.from(all.querySelectorAll('*')).filter((el) => visible(el) && scrollable(el, 'y'))
  for (const el of scrollers) {
    let ancestor = el.parentElement
    while (ancestor) {
      if (scrollable(ancestor, 'y')) {
        const share = el.clientHeight / Math.max(1, ancestor.clientHeight)
        if (share > options.nestedScrollShare) {
          findings.push({
            rule: 'nested-scroll',
            severity: 'error',
            target: describe(el),
            detail: `该滚动容器占了祖先滚动区 ${(share * 100).toFixed(0)}% 的高度（${describe(ancestor)}），滚轮落在哪一层取决于指针位置。`,
          })
        }
        break
      }
      ancestor = ancestor.parentElement
    }
  }

  // ---- 被裁掉 / 滚不到的交互元素 -----------------------------------------
  const interactive = Array.from(all.querySelectorAll('button, a[href], input, select, textarea, [role="button"]'))
  for (const el of interactive) {
    if (!visible(el)) continue
    const rect = el.getBoundingClientRect()

    // 包在 label 里的勾选框本身只有 16px，但整行 label 都可点，不算小目标。
    const label = el.closest('label')
    const hitRect = label ? label.getBoundingClientRect() : rect
    if (hitRect.width < options.minTargetSize || hitRect.height < options.minTargetSize) {
      findings.push({
        rule: 'tiny-target',
        severity: 'warn',
        target: describe(el),
        detail: `可点区域只有 ${Math.round(hitRect.width)}×${Math.round(hitRect.height)}px，小于 ${options.minTargetSize}px。`,
      })
    }

    let clipper: Element | null = el.parentElement
    let reachable = rect.top >= -1 && rect.bottom <= viewport.height + 1
    while (clipper && !reachable) {
      if (scrollable(clipper, 'y')) {
        reachable = true
        break
      }
      if (clips(clipper, 'y')) {
        const clipRect = clipper.getBoundingClientRect()
        if (rect.bottom > clipRect.bottom + 1 || rect.top < clipRect.top - 1) {
          findings.push({
            rule: 'clipped-action',
            severity: 'error',
            target: describe(el),
            detail: `控件超出 ${describe(clipper)} 的可视范围且该容器不能滚动，用户永远点不到。`,
          })
        }
        break
      }
      clipper = clipper.parentElement
    }
    if (!reachable && !clipper) {
      findings.push({
        rule: 'clipped-action',
        severity: 'error',
        target: describe(el),
        detail: `控件位于视口之外（top=${Math.round(rect.top)} bottom=${Math.round(rect.bottom)}），且没有可滚动祖先。`,
      })
    }

    // 命中测试：勾选框的对勾被绝对定位的 input 盖住那类问题只有这样才抓得到。
    const cx = Math.min(viewport.width - 1, Math.max(0, rect.left + rect.width / 2))
    const cy = Math.min(viewport.height - 1, Math.max(0, rect.top + rect.height / 2))
    // 滚动容器边缘那一行只露出一半是正常的，中心点落在容器之外时不做命中测试。
    let insideClips = true
    for (let node = el.parentElement; node && insideClips; node = node.parentElement) {
      if (!clips(node, 'y') && !clips(node, 'x')) continue
      const clipRect = node.getBoundingClientRect()
      insideClips = cy >= clipRect.top && cy <= clipRect.bottom && cx >= clipRect.left && cx <= clipRect.right
    }
    if (insideClips && rect.top >= 0 && rect.bottom <= viewport.height) {
      const hit = document.elementFromPoint(cx, cy)
      if (hit && hit !== el && !el.contains(hit) && !hit.contains(el)) {
        findings.push({
          rule: 'occluded',
          severity: 'error',
          target: describe(el),
          detail: `中心点被 ${describe(hit)} 遮挡。`,
        })
      }
    }
  }

  // ---- 实际被截断的文本 ---------------------------------------------------
  for (const el of Array.from(all.querySelectorAll('*'))) {
    if (!visible(el)) continue
    const style = getComputedStyle(el)
    if (style.textOverflow !== 'ellipsis') continue
    if (el.scrollWidth <= el.clientWidth + 1) continue
    const ratio = el.clientWidth / el.scrollWidth
    // 密集表格里切掉一两成尾巴是常态，只在真的读不出内容时才报。
    if (ratio >= 0.8) continue
    // truncate + title 是可接受的：省略号后面的内容悬停就能拿到完整值。
    if ((el.getAttribute('title') || el.getAttribute('aria-label') || '').trim()) continue
    findings.push({
      rule: 'truncated-text',
      severity: ratio < 0.6 ? 'error' : 'warn',
      target: describe(el),
      detail: `文本只显示了 ${(ratio * 100).toFixed(0)}%（${el.clientWidth}px / ${el.scrollWidth}px）。`,
    })
  }

  return {
    findings,
    metrics: {
      viewport,
      pageKind,
      contentBottom: Math.round(contentBottom),
      deadSpace: Math.round(deadSpace),
      widthUsage: Number(widthUsage.toFixed(3)),
      scrollContainers: scrollers.length,
    },
  }
}
