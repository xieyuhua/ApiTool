// ApiTool 请求捕获扩展 - 后台 Service Worker
// 使用 chrome.debugger（DevTools 协议）监听指定标签页的网络请求，
// 捕获 方法/URL/请求头/Query/请求体/响应状态/响应头/响应体，
// 回传到 ApiTool 桌面端的捕获服务（默认 http://127.0.0.1:8653）。

const DEFAULT_ENDPOINT = 'http://127.0.0.1:8653';

let settings = {
  endpoint: DEFAULT_ENDPOINT,
  token: '',
  patterns: [],          // 监控网址规则，支持 * 通配，如 https://api.example.com/* 或 example.com
  enabled: false,        // 总开关
  captureResponse: true, // 是否抓取响应体
};

const attached = new Set();   // 已 attach 的 tabId
const tabPages = {};          // tabId -> 页面 URL
const pending = new Map();    // requestId -> 部分捕获记录

// ---------------- 设置读写 ----------------
function loadSettings() {
  return new Promise((resolve) => {
    chrome.storage.local.get(['apitool_capture'], (res) => {
      if (res.apitool_capture) {
        settings = Object.assign(settings, res.apitool_capture);
      }
      resolve(settings);
    });
  });
}
function saveSettings() {
  chrome.storage.local.set({ apitool_capture: settings });
}

// ---------------- 规则匹配 ----------------
function escapeRegExp(s) {
  return s.replace(/[.+?^${}()|[\]\\]/g, '\\$&');
}
function matchUrl(url) {
  if (!url) return false;
  const pats = (settings.patterns || []).map(p => p.trim()).filter(Boolean);
  if (pats.length === 0) return false; // 未配置任何规则时不捕获
  for (const p of pats) {
    const re = new RegExp(p.split('*').map(escapeRegExp).join('.*'));
    if (re.test(url)) return true;
  }
  return false;
}

// ---------------- 调试器连接管理 ----------------
function ensureAttached(tabId, url) {
  if (!settings.enabled) return;
  if (attached.has(tabId)) return;
  if (!matchUrl(url)) return;
  chrome.debugger.attach({ tabId }, '1.3', () => {
    if (chrome.runtime.lastError) {
      console.warn('[ApiTool] attach 失败', tabId, chrome.runtime.lastError.message);
      return;
    }
    attached.add(tabId);
    chrome.debugger.sendCommand({ tabId }, 'Network.enable', {}, () => {
      if (chrome.runtime.lastError) console.warn('[ApiTool] Network.enable 失败', chrome.runtime.lastError.message);
    });
  });
}

function detach(tabId) {
  if (!attached.has(tabId)) return;
  chrome.debugger.detach({ tabId }, () => {
    // 忽略已断开的错误
  });
  attached.delete(tabId);
  delete tabPages[tabId];
}

function detachAll() {
  for (const tabId of Array.from(attached)) detach(tabId);
}

// ---------------- 事件监听 ----------------
chrome.tabs.onUpdated.addListener((tabId, changeInfo, tab) => {
  if (changeInfo.url) tabPages[tabId] = changeInfo.url;
  if (tab.url) tabPages[tabId] = tab.url;
  if (settings.enabled) {
    if (matchUrl(tab.url)) ensureAttached(tabId, tab.url);
    else detach(tabId);
  }
});

chrome.tabs.onCreated.addListener((tab) => {
  if (tab.url) tabPages[tab.id] = tab.url;
  if (settings.enabled && matchUrl(tab.url)) ensureAttached(tab.id, tab.url);
});

chrome.tabs.onRemoved.addListener((tabId) => {
  detach(tabId);
});

chrome.debugger.onDetach.addListener((source) => {
  if (source && source.tabId) {
    attached.delete(source.tabId);
    delete tabPages[source.tabId];
  }
});

// ---------------- 网络事件捕获 ----------------
chrome.debugger.onEvent.addListener((source, method, params) => {
  const tabId = source && source.tabId;
  if (!tabId) return;

  if (method === 'Network.requestWillBeSent') {
    const req = params.request || {};
    const headers = objToKV(req.headers || {});
    let bodyType = 'none';
    let body = req.postData || '';
    if (body) {
      const ct = lowerHeader(req.headers, 'content-type') || '';
      if (ct.includes('application/json') || body.trim().startsWith('{') || body.trim().startsWith('[')) bodyType = 'json';
      else if (ct.includes('x-www-form-urlencoded') || (body.includes('=') && !body.trim().startsWith('{'))) bodyType = 'form';
      else bodyType = 'text';
    }
    pending.set(params.requestId, {
      method: (req.method || 'GET').toUpperCase(),
      url: req.url || '',
      headers,
      bodyType,
      body,
      pageUrl: tabPages[tabId] || params.documentURL || '',
      matchedUrl: matchedRule(req.url),
      _rid: params.requestId,
      statusCode: 0,
      statusText: '',
      respHeaders: {},
      respBody: '',
      respIsJson: false,
      durationMs: 0,
      start: Date.now(),
      error: '',
    });
  } else if (method === 'Network.responseReceived') {
    const p = pending.get(params.requestId);
    if (!p) return;
    const resp = params.response || {};
    p.statusCode = resp.status || 0;
    p.statusText = resp.statusText || '';
    p.respHeaders = resp.headers || {};
    p._mime = resp.mimeType || '';
  } else if (method === 'Network.loadingFinished') {
    const p = pending.get(params.requestId);
    if (!p) return;
    p.durationMs = Date.now() - p.start;
    if (settings.captureResponse) {
      chrome.debugger.sendCommand(source, 'Network.getResponseBody', { requestId: params.requestId }, (result) => {
        if (!chrome.runtime.lastError && result) {
          let b = result.body || '';
          if (result.base64Encoded) {
            try { b = decodeBase64(b); } catch (e) { b = ''; }
          }
          p.respBody = b;
          p.respIsJson = isJsonLike(p._mime, b);
        }
        finalize(p);
      });
    } else {
      finalize(p);
    }
  } else if (method === 'Network.loadingFailed') {
    const p = pending.get(params.requestId);
    if (!p) return;
    p.error = (params.errorText || '请求失败');
    p.durationMs = Date.now() - p.start;
    finalize(p);
  }
});

function finalize(p) {
  delete p.start;
  delete p._mime;
  const rid = p._rid;
  delete p._rid;
  sendCapture(p);
  if (rid) pending.delete(rid);
}

function matchedRule(url) {
  const pats = (settings.patterns || []).map(p => p.trim()).filter(Boolean);
  for (const p of pats) {
    const re = new RegExp(p.split('*').map(escapeRegExp).join('.*'));
    if (re.test(url)) return p;
  }
  return '';
}

// ---------------- 回传桌面端 ----------------
function sendCapture(p) {
  const endpoint = (settings.endpoint || DEFAULT_ENDPOINT).replace(/\/+$/, '');
  const url = endpoint + '/capture';
  const payload = { requests: [p] };
  fetch(url, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'Authorization': 'Bearer ' + (settings.token || ''),
    },
    body: JSON.stringify(payload),
  }).catch((e) => {
    console.warn('[ApiTool] 回传失败', e && e.message);
  });
}

// ---------------- 工具函数 ----------------
function objToKV(obj) {
  const out = [];
  for (const k of Object.keys(obj || {})) {
    out.push({ key: k, value: String(obj[k]), enabled: true });
  }
  return out;
}
function lowerHeader(headers, name) {
  if (!headers) return '';
  for (const k of Object.keys(headers)) {
    if (k.toLowerCase() === name) return headers[k];
  }
  return '';
}
function isJsonLike(mime, body) {
  if (/json/i.test(mime || '')) return true;
  const t = (body || '').trim();
  if (!t) return false;
  try { JSON.parse(t); return true; } catch (e) { return false; }
}
function decodeBase64(s) {
  // atob 解码为二进制串，再转 UTF-8
  const bin = atob(s);
  const bytes = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
  return new TextDecoder('utf-8').decode(bytes);
}

// ---------------- 启动 ----------------
loadSettings().then(() => {
  if (settings.enabled) {
    // 重新连接当前已打开且命中的标签页
    chrome.tabs.query({}, (tabs) => {
      for (const t of tabs) {
        if (t.url) tabPages[t.id] = t.url;
        if (matchUrl(t.url)) ensureAttached(t.id, t.url);
      }
    });
  }
});

// 设置变更时同步连接状态
chrome.storage.onChanged.addListener((changes, area) => {
  if (area === 'local' && changes.apitool_capture) {
    const old = settings;
    settings = Object.assign(settings, changes.apitool_capture.newValue || {});
    if (settings.enabled && !old.enabled) {
      chrome.tabs.query({}, (tabs) => {
        for (const t of tabs) {
          if (t.url) tabPages[t.id] = t.url;
          if (matchUrl(t.url)) ensureAttached(t.id, t.url);
        }
      });
    } else if (!settings.enabled && old.enabled) {
      detachAll();
    }
  }
});

// 接收弹窗指令
chrome.runtime.onMessage.addListener((msg) => {
  if (!msg || !msg.type) return;
  if (msg.type === 'reconnect' || msg.type === 'apply') {
    chrome.tabs.query({}, (tabs) => {
      for (const t of tabs) {
        if (t.url) tabPages[t.id] = t.url;
        if (settings.enabled && matchUrl(t.url)) ensureAttached(t.id, t.url);
        else if (!settings.enabled) detach(t.id);
      }
    });
  } else if (msg.type === 'disconnect') {
    detachAll();
  }
});
