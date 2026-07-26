// 通过 new Function 在受限作用域内执行用户脚本（本地桌面工具，非沙箱安全场景）
export function runScript(code, sandbox) {
  if (!code || !code.trim()) return
  const fn = new Function('request', 'response', 'env', 'setEnv', 'console', '"use strict";\n' + code)
  fn(sandbox.request, sandbox.response, sandbox.env, sandbox.setEnv, sandbox.console || console)
}
