// ApiTool 请求捕获扩展 - 弹窗逻辑
const $ = (id) => document.getElementById(id);

function load() {
  chrome.storage.local.get(['apitool_capture'], (res) => {
    const s = res.apitool_capture || {};
    $('endpoint').value = s.endpoint || 'http://127.0.0.1:8653';
    $('token').value = s.token || '';
    $('patterns').value = (s.patterns || []).join('\n');
    $('enabled').checked = !!s.enabled;
    $('captureResponse').checked = s.captureResponse !== false;
  });
}

function collect() {
  return {
    endpoint: $('endpoint').value.trim() || 'http://127.0.0.1:8653',
    token: $('token').value.trim(),
    patterns: $('patterns').value.split('\n').map(x => x.trim()).filter(Boolean),
    enabled: $('enabled').checked,
    captureResponse: $('captureResponse').checked,
  };
}

function setStatus(msg, type) {
  const el = $('status');
  el.textContent = msg;
  el.className = 'status' + (type ? ' ' + type : '');
}

$('save').addEventListener('click', () => {
  const s = collect();
  chrome.storage.local.set({ apitool_capture: s }, () => {
    setStatus('已保存' + (s.enabled ? '，并开始捕获' : '（未启用）'), 'ok');
    // 通知 background 立即应用
    chrome.runtime.sendMessage({ type: 'apply' }).catch(() => {});
  });
});

$('test').addEventListener('click', async () => {
  const s = collect();
  const url = (s.endpoint || 'http://127.0.0.1:8653').replace(/\/+$/, '') + '/health';
  setStatus('正在测试连接…');
  try {
    const r = await fetch(url, { method: 'GET' });
    if (r.ok) {
      const j = await r.json().catch(() => ({}));
      setStatus('连接成功 ✅ 捕获服务端口：' + (j.port || '8653'), 'ok');
    } else {
      setStatus('连接被拒绝（HTTP ' + r.status + '），请确认桌面端已启动捕获服务', 'err');
    }
  } catch (e) {
    setStatus('无法连接：' + e.message + '，请确认桌面端已启动「请求捕获」服务', 'err');
  }
});

$('reconnect').addEventListener('click', () => {
  chrome.runtime.sendMessage({ type: 'reconnect' }).catch(() => {});
  setStatus('已向后台发送重新连接指令', 'ok');
});

$('disconnect').addEventListener('click', () => {
  chrome.runtime.sendMessage({ type: 'disconnect' }).catch(() => {});
  setStatus('已断开全部调试连接', 'ok');
});

// 接收后台指令回执
chrome.runtime.onMessage.addListener((msg) => {
  if (msg && msg.type === 'status' && msg.text) setStatus(msg.text, msg.ok ? 'ok' : 'err');
});

load();
