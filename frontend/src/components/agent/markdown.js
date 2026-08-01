// 轻量 Markdown 渲染（无外部依赖），支持：标题、粗体/斜体/行内代码、
// 代码块（含 ```mermaid 图表占位）、有序/无序列表、引用、表格、链接、水平线。
// mermaid 通过 CDN 动态加载，加载失败时降级为代码块显示源码。

function escapeHtml(s) {
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
}

function inline(text) {
  let t = escapeHtml(text)
  // 行内代码
  t = t.replace(/`([^`]+)`/g, '<code class="md-code-inline">$1</code>')
  // 粗体
  t = t.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
  // 斜体
  t = t.replace(/(^|[^*])\*([^*]+)\*/g, '$1<em>$2</em>')
  // 链接
  t = t.replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<a href="$2" target="_blank" rel="noopener">$1</a>')
  return t
}

let mermaidPromise = null
let mermaidSeq = 0

function loadMermaid() {
  if (window.mermaid) return Promise.resolve(window.mermaid)
  if (mermaidPromise) return mermaidPromise
  mermaidPromise = new Promise((resolve, reject) => {
    const s = document.createElement('script')
    s.src = 'https://cdn.jsdelivr.net/npm/mermaid@10/dist/mermaid.min.js'
    s.onload = () => {
      try {
        window.mermaid.initialize({ startOnLoad: false, theme: document.documentElement.classList.contains('dark') ? 'dark' : 'default' })
        resolve(window.mermaid)
      } catch (e) { reject(e) }
    }
    s.onerror = () => reject(new Error('mermaid 加载失败'))
    document.head.appendChild(s)
  })
  return mermaidPromise
}

// 渲染页面中所有未处理的 mermaid 图表容器
export async function renderMermaid(root) {
  const nodes = (root || document).querySelectorAll('.md-mermaid[data-src]:not([data-done])')
  if (!nodes.length) return
  let mermaid
  try {
    mermaid = await loadMermaid()
  } catch {
    nodes.forEach(n => {
      n.setAttribute('data-done', '1')
      const src = decodeURIComponent(n.getAttribute('data-src'))
      n.innerHTML = '<pre class="md-pre"><code>' + escapeHtml(src) + '</code></pre><div class="md-mermaid-tip">（图表渲染需要网络加载 mermaid，已显示源码）</div>'
    })
    return
  }
  for (const n of nodes) {
    n.setAttribute('data-done', '1')
    const src = decodeURIComponent(n.getAttribute('data-src'))
    try {
      const id = 'mmd_' + (++mermaidSeq)
      const { svg } = await mermaid.render(id, src)
      n.innerHTML = svg
    } catch (e) {
      n.innerHTML = '<pre class="md-pre"><code>' + escapeHtml(src) + '</code></pre><div class="md-mermaid-tip">图表语法有误</div>'
    }
  }
}

export function renderMarkdown(md) {
  if (!md) return ''
  const lines = String(md).replace(/\r\n/g, '\n').split('\n')
  const out = []
  let i = 0
  while (i < lines.length) {
    let line = lines[i]

    // 代码块
    const fence = line.match(/^```\s*(\w+)?\s*$/)
    if (fence) {
      const lang = (fence[1] || '').toLowerCase()
      const buf = []
      i++
      while (i < lines.length && !/^```\s*$/.test(lines[i])) {
        buf.push(lines[i]); i++
      }
      i++ // 跳过结束 ```
      const code = buf.join('\n')
      if (lang === 'mermaid') {
        out.push('<div class="md-mermaid" data-src="' + encodeURIComponent(code) + '">图表渲染中…</div>')
      } else {
        out.push('<pre class="md-pre"><code class="lang-' + escapeHtml(lang) + '">' + escapeHtml(code) + '</code></pre>')
      }
      continue
    }

    // 表格
    if (/\|/.test(line) && i + 1 < lines.length && /^\s*\|?[\s:|-]+\|?\s*$/.test(lines[i + 1]) && /-/.test(lines[i + 1])) {
      const header = line.split('|').map(s => s.trim()).filter((s, idx, arr) => !(idx === 0 && s === '') && !(idx === arr.length - 1 && s === ''))
      i += 2
      const rows = []
      while (i < lines.length && /\|/.test(lines[i]) && lines[i].trim() !== '') {
        const cells = lines[i].split('|').map(s => s.trim())
        if (cells[0] === '') cells.shift()
        if (cells[cells.length - 1] === '') cells.pop()
        rows.push(cells)
        i++
      }
      let html = '<table class="md-table"><thead><tr>'
      header.forEach(h => { html += '<th>' + inline(h) + '</th>' })
      html += '</tr></thead><tbody>'
      rows.forEach(r => {
        html += '<tr>'
        header.forEach((_, idx) => { html += '<td>' + inline(r[idx] || '') + '</td>' })
        html += '</tr>'
      })
      html += '</tbody></table>'
      out.push(html)
      continue
    }

    // 标题
    const h = line.match(/^(#{1,6})\s+(.*)$/)
    if (h) {
      const lv = h[1].length
      out.push('<h' + lv + ' class="md-h md-h' + lv + '">' + inline(h[2]) + '</h' + lv + '>')
      i++; continue
    }

    // 水平线
    if (/^\s*([-*_])\1{2,}\s*$/.test(line)) {
      out.push('<hr class="md-hr" />'); i++; continue
    }

    // 引用
    if (/^\s*>\s?/.test(line)) {
      const buf = []
      while (i < lines.length && /^\s*>\s?/.test(lines[i])) {
        buf.push(lines[i].replace(/^\s*>\s?/, '')); i++
      }
      out.push('<blockquote class="md-quote">' + inline(buf.join('\n')).replace(/\n/g, '<br/>') + '</blockquote>')
      continue
    }

    // 无序列表
    if (/^\s*[-*+]\s+/.test(line)) {
      const buf = []
      while (i < lines.length && /^\s*[-*+]\s+/.test(lines[i])) {
        buf.push('<li>' + inline(lines[i].replace(/^\s*[-*+]\s+/, '')) + '</li>'); i++
      }
      out.push('<ul class="md-ul">' + buf.join('') + '</ul>')
      continue
    }

    // 有序列表
    if (/^\s*\d+\.\s+/.test(line)) {
      const buf = []
      while (i < lines.length && /^\s*\d+\.\s+/.test(lines[i])) {
        buf.push('<li>' + inline(lines[i].replace(/^\s*\d+\.\s+/, '')) + '</li>'); i++
      }
      out.push('<ol class="md-ol">' + buf.join('') + '</ol>')
      continue
    }

    // 空行
    if (line.trim() === '') { i++; continue }

    // 普通段落（合并连续非空行）
    const buf = [line]
    i++
    while (i < lines.length && lines[i].trim() !== '' &&
      !/^```/.test(lines[i]) && !/^(#{1,6})\s/.test(lines[i]) &&
      !/^\s*[-*+]\s+/.test(lines[i]) && !/^\s*\d+\.\s+/.test(lines[i]) &&
      !/^\s*>\s?/.test(lines[i])) {
      buf.push(lines[i]); i++
    }
    out.push('<p class="md-p">' + inline(buf.join('\n')).replace(/\n/g, '<br/>') + '</p>')
  }
  return out.join('\n')
}
