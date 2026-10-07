// Agent 后端桥接。直接调用 window.go.main.App 上的方法，
// 避免依赖 wailsjs 自动生成的绑定文件（build 时才会更新）。

function app() {
  const a = window.go && window.go.main && window.go.main.App
  if (!a) throw new Error('未检测到桌面运行时（Wails 桥接），请在应用内使用')
  return a
}

export const AgentAPI = {
  load() { return app().LoadAgentData() },
  saveConfig(cfg) { return app().SaveAgentConfig(cfg) },
  saveSkills(skills) { return app().SaveAgentSkills(skills) },
  saveServers(servers) { return app().SaveMCPServers(servers) },
  saveUsers(users) { return app().SaveAgentUsers(users) },
  clearMessages() { return app().ClearAgentMessages() },
  createSession(title) { return app().CreateAgentSession(title || '') },
  switchSession(id) { return app().SwitchAgentSession(id) },
  deleteSession(id) { return app().DeleteAgentSession(id) },
  renameSession(id, title) { return app().RenameAgentSession(id, title) },
  exportSession(id, format) { return app().ExportAgentSession(id || '', format) },
  webChatInfo() { return app().WebChatInfo() },
  startWebChat(port) { return app().StartWebChat(port || '8090') },
  stopWebChat() { return app().StopWebChat() },
  resetWebChatToken() { return app().ResetWebChatToken() },
  run(args) { return app().RunAgent(args) },
  polish(args) { return app().PolishText(args) },
  listTools() { return app().ListAllMCPTools() },
  getBuiltinTools() { return app().GetBuiltinTools() },
  testServer(srv) { return app().TestMCPServer(srv) },
  queryLogs(args) { return app().QueryAgentLogs(args) },
  clearLogs() { return app().ClearAgentLogs() },
  getAgentLog(id) { return app().GetAgentLog(id) },
  logFacets() { return app().GetAgentLogFacets() },
}

export function hasBridge() {
  return !!(window.go && window.go.main && window.go.main.App && window.go.main.App.RunAgent)
}

// 是否运行在局域网网页端（httpBridge 已装上 HTTP/SSE 兼容桥）。
//
// 判据说明：桌面端由 Wails 注入 window.go + window.runtime，installWebBridge()
// 检测到这两个对象会直接 return false 且**不会**设置 __WEBUI__；
// 只有走 agent.html（局域网网页入口）时才会装桥并打上该标记。
// 因此它是「桌面 / 网页」的唯一可靠判据，UI 上所有桌面专属入口都应据此隐藏。
export function isWebUI() {
  return typeof window !== 'undefined' && !!window.__WEBUI__
}
