// 命令行 / curl 解析与生成工具
// 支持：curl、httpie(cmd)、bash(变量拼接)、powershell(Invoke-RestMethod) 的互相转换

// 去除行尾续行符（\ 后换行）并合并为一行命令
function unwrapContinuation(text) {
  return text.replace(/\\\s*\n/g, ' ').replace(/\^\s*\n/g, ' ').replace(/`\s*\n/g, ' ')
}

// 去掉字符串两端的引号（支持单/双/无引号）
function stripQuotes(s) {
  if (!s) return s
  const t = s.trim()
  if ((t.startsWith('"') && t.endsWith('"')) || (t.startsWith("'") && t.endsWith("'"))) {
    return t.slice(1, -1)
  }
  return t
}

// 将一段命令拆分成 token（考虑单/双引号）
function tokenize(cmd) {
  const tokens = []
  let i = 0
  let cur = ''
  let quote = ''
  while (i < cmd.length) {
    const c = cmd[i]
    if (quote) {
      if (c === quote) { quote = '' }
      else cur += c
    } else if (c === '"' || c === "'") {
      quote = c
    } else if (/\s/.test(c)) {
      if (cur) { tokens.push(cur); cur = '' }
    } else {
      cur += c
    }
    i++
  }
  if (cur) tokens.push(cur)
  return tokens
}

// 判断字符串是否像 URL（含 :// 或 http 开头）
function isUrlLike(s) {
  return /^https?:\/\//i.test(s) || /^www\./i.test(s)
}

// 在 token 列表中找出首个 URL
function findUrl(tokens) {
  for (const t of tokens) {
    if (isUrlLike(t)) return stripQuotes(t)
  }
  return ''
}

// 解析常见的命令行格式，返回 { method, url, headers, query, body }
export function parseCli(text) {
  const raw = (text || '').trim()
  if (!raw) return null
  const cmd = unwrapContinuation(raw)
  const tokens = tokenize(cmd)
  if (!tokens.length) return null

  const lower0 = (tokens[0] || '').toLowerCase()
  const isCurl = lower0 === 'curl' || lower0 === 'curl.exe'
  const isPwsh = /invoke-restmethod|irm\b/i.test(lower0) || lower0.startsWith('invoke')
  const isHttpie = lower0 === 'http' || lower0 === 'https' || lower0 === 'httpie'

  const result = { method: 'GET', url: '', headers: {}, query: {}, body: '', bodyType: 'none' }

  if (isCurl) {
    parseCurl(tokens, result)
  } else if (isPwsh) {
    parsePowershell(cmd, tokens, result)
  } else if (isHttpie) {
    parseHttpie(tokens, result)
  } else {
    // 未知格式：尽力提取 URL 与方法
    result.url = findUrl(tokens)
    const m = raw.match(/-X\s+(\w+)/i) || raw.match(/--request\s+(\w+)/i)
    if (m) result.method = m[1].toUpperCase()
  }

  // URL 上的 query 解析进 query 对象
  if (result.url) {
    try {
      const u = new URL(result.url)
      result.url = u.origin + u.pathname
      u.searchParams.forEach((v, k) => { result.query[k] = v })
    } catch { /* 非标准 URL，保留原样 */ }
  }

  // 规范化 headers 为数组
  result.headersArr = Object.entries(result.headers).map(([key, value]) => ({ key, value }))
  result.queryArr = Object.entries(result.query).map(([key, value]) => ({ key, value }))
  return result
}

function parseCurl(tokens, r) {
  let i = 1
  while (i < tokens.length) {
    const t = tokens[i]
    const tl = t.toLowerCase()
    if (tl === '-x' || tl === '--request') {
      r.method = (tokens[i + 1] || 'GET').toUpperCase(); i += 2
    } else if (tl === '-g' || tl === '--get') {
      r.method = 'GET'; i++
    } else if (tl === '-i' || tl === '--head') {
      r.method = 'HEAD'; i++
    } else if (tl === '-d' || tl === '--data' || tl === '--data-raw' || tl === '--data-binary' || tl === '--data-urlencode' || tl === '--data-ascii') {
      const val = stripQuotes(tokens[i + 1] || '')
      r.body = val; r.bodyType = 'text'; i += 2
    } else if (tl === '-f' || tl === '--form') {
      const val = stripQuotes(tokens[i + 1] || '')
      r.body = val; r.bodyType = 'form'; i += 2
    } else if (tl === '-h' || tl === '--header') {
      const h = stripQuotes(tokens[i + 1] || '')
      const idx = h.indexOf(':')
      if (idx > 0) r.headers[h.slice(0, idx).trim()] = h.slice(idx + 1).trim()
      i += 2
    } else if (tl === '-u' || tl === '--user') {
      const u = stripQuotes(tokens[i + 1] || '')
      r.headers['Authorization'] = 'Basic ' + (typeof btoa !== 'undefined' ? btoa(u) : Buffer.from(u).toString('base64'))
      i += 2
    } else if (tl === '--url') {
      r.url = stripQuotes(tokens[i + 1] || ''); i += 2
    } else if (tl === '-k' || tl === '--insecure') {
      i++
    } else if (isUrlLike(t) && !r.url) {
      r.url = stripQuotes(t); i++
    } else {
      i++
    }
  }
  // 若 -d 存在但无 -X，curl 默认 POST
  if (r.body && r.method === 'GET') r.method = 'POST'
  // 根据 content-type / body 猜测 bodyType
  guessBodyType(r)
}

function parsePowershell(cmd, tokens, r) {
  const m = cmd.match(/(?:Uri|Url)\s*[=:]+\s*['"]?([^'"\s]+)['"]?/i)
  if (m) r.url = m[1]
  const mm = cmd.match(/-Method\s+['"]?(\w+)['"]?/i)
  if (mm) r.method = mm[1].toUpperCase()
  const mb = cmd.match(/-Body\s+([@$][A-Za-z0-9_]+|['"][^'"]*['"]|@\{[^}]*\})/i)
  if (mb) {
    const b = mb[1]
    if (b.startsWith("'") || b.startsWith('"')) r.body = stripQuotes(b)
    else r.body = b // 变量名，原样保留说明
    r.bodyType = 'text'
  }
  // Headers
  const hm = cmd.match(/-Headers\s+(\$\{[^}]*\}|\{[^\}]*\}|@\{[^}]*\})/i)
  if (hm) {
    const inner = hm[1].replace(/^[\$\{@]+/, '').replace(/\}$/, '')
    const re = /['"]?([\w-]+)['"]?\s*=\s*['"]?([^'";}]+)['"]?/g
    let mm2
    while ((mm2 = re.exec(inner)) !== null) r.headers[mm2[1]] = mm2[2].trim()
  }
  // 直接用 -Headers @{ "k"="v" } 的逐个匹配
  const re2 = /-Headers\s*\{([^}]*)\}/gi
  let rm
  while ((rm = re2.exec(cmd)) !== null) {
    const inner = rm[1]
    const re3 = /['"]?([\w-]+)['"]?\s*=\s*['"]?([^'";}]+)['"]?/g
    let x
    while ((x = re3.exec(inner)) !== null) r.headers[x[1]] = x[2].trim()
  }
  guessBodyType(r)
}

function parseHttpie(tokens, r) {
  // httpie: http POST url Header:value body:=json
  if (/^(http|https)$/i.test(tokens[0])) {
    const m = (tokens[1] || '').toUpperCase()
    if (['GET', 'POST', 'PUT', 'DELETE', 'PATCH', 'HEAD', 'OPTIONS'].includes(m)) {
      r.method = m; tokens.splice(1, 1)
    }
  }
  let i = 1
  while (i < tokens.length) {
    const t = tokens[i]
    if (isUrlLike(t) && !r.url) { r.url = stripQuotes(t); i++; continue }
    if (t.includes(':') && !t.startsWith('http')) {
      const idx = t.indexOf(':')
      const k = t.slice(0, idx).trim()
      let v = t.slice(idx + 1).trim()
      // 处理 := (json) 或 = (表单)
      if (k.endsWith('=')) {
        r.headers[k.slice(0, -1)] = v
      } else {
        r.headers[k] = v
        if (v.startsWith('{') || v.startsWith('[')) { r.body = v; r.bodyType = 'json' }
        else if (t.includes('==')) { r.query[t.slice(0, t.indexOf('=='))] = v }
      }
      i++
    } else {
      i++
    }
  }
  if (Object.keys(r.headers).length || r.body) guessBodyType(r)
}

function guessBodyType(r) {
  if (!r.body) { r.bodyType = 'none'; return }
  const ct = Object.entries(r.headers).find(([k]) => k.toLowerCase() === 'content-type')
  if (ct) {
    if (/json/i.test(ct[1])) r.bodyType = 'json'
    else if (/x-www-form-urlencoded|form/i.test(ct[1])) r.bodyType = 'form'
    else r.bodyType = 'text'
  } else if (r.body.trim().startsWith('{') || r.body.trim().startsWith('[')) {
    r.bodyType = 'json'
  } else {
    r.bodyType = r.bodyType === 'form' ? 'form' : 'text'
  }
}

// ---------------- 生成 ----------------

function shellEscape(s) {
  // bash / curl：用单引号包裹，内部单引号转义
  return "'" + String(s).replace(/'/g, "'\\''") + "'"
}
function pwshEscape(s) {
  return '"' + String(s).replace(/"/g, '`"') + '"'
}

// 生成 curl 命令
export function toCurl(req) {
  const lines = ["curl -X " + req.method + " \\"]
  lines.push("  " + shellEscape(req.url) + " \\")
  for (const h of req.headersArr || []) {
    lines.push("  -H " + shellEscape(h.key + ': ' + h.value) + " \\")
  }
  if (req.body) {
    const bt = req.bodyType || 'text'
    const flag = bt === 'form' ? '-F' : '-d'
    lines.push("  " + flag + " " + shellEscape(req.body) + " \\")
  }
  // 去掉最后一个续行符
  let out = lines.join('\n')
  out = out.replace(/\s+\\$/, '')
  return out
}

// 生成 bash（变量拼接风格）
export function toBash(req) {
  const lines = []
  lines.push("URL=" + shellEscape(req.url))
  lines.push("METHOD=" + shellEscape(req.method))
  lines.push('HEADERS=(')
  for (const h of req.headersArr || []) {
    lines.push("  -H " + shellEscape(h.key + ': ' + h.value))
  }
  lines.push(')')
  let cmd = "curl -X \"$METHOD\" \"$URL\""
  for (const h of req.headersArr || []) { cmd += " -H " + shellEscape(h.key + ': ' + h.value) }
  if (req.body) {
    const flag = (req.bodyType === 'form') ? '-F' : '-d'
    cmd += " " + flag + " " + shellEscape(req.body)
  }
  lines.push(cmd)
  return lines.join('\n')
}

// 生成 cmd（Windows 命令提示符，^ 续行）
export function toCmd(req) {
  const lines = ["curl -X " + req.method + " ^"]
  lines.push("  " + shellEscape(req.url) + " ^")
  for (const h of req.headersArr || []) {
    lines.push("  -H " + shellEscape(h.key + ': ' + h.value) + " ^")
  }
  if (req.body) {
    const flag = (req.bodyType === 'form') ? '-F' : '-d'
    lines.push("  " + flag + " " + shellEscape(req.body) + " ^")
  }
  let out = lines.join('\n').replace(/\s+\^$/, '')
  return out
}

// 生成 powershell（Invoke-RestMethod）
export function toPowershell(req) {
  const lines = []
  lines.push('$url = ' + pwshEscape(req.url))
  lines.push('$method = ' + pwshEscape(req.method))
  if (req.headersArr && req.headersArr.length) {
    lines.push('$headers = @{')
    for (const h of req.headersArr) {
      lines.push('  ' + pwshEscape(h.key) + ' = ' + pwshEscape(h.value))
    }
    lines.push('}')
  }
  let invoke = 'Invoke-RestMethod -Uri $url -Method $method'
  if (req.headersArr && req.headersArr.length) invoke += ' -Headers $headers'
  if (req.body) {
    const flag = (req.bodyType === 'form') ? '-Form' : '-Body'
    invoke += ' ' + flag + ' ' + pwshEscape(req.body)
  }
  lines.push(invoke)
  return lines.join('\n')
}

export const FORMATS = [
  { key: 'curl', label: 'cURL', gen: toCurl },
  { key: 'bash', label: 'Bash', gen: toBash },
  { key: 'cmd', label: 'CMD', gen: toCmd },
  { key: 'powershell', label: 'PowerShell', gen: toPowershell },
]
