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
  run(args) { return app().RunAgent(args) },
  polish(args) { return app().PolishText(args) },
  listTools() { return app().ListAllMCPTools() },
  getBuiltinTools() { return app().GetBuiltinTools() },
  testServer(srv) { return app().TestMCPServer(srv) },
  queryLogs(args) { return app().QueryAgentLogs(args) },
  clearLogs() { return app().ClearAgentLogs() },
}

export function hasBridge() {
  return !!(window.go && window.go.main && window.go.main.App && window.go.main.App.RunAgent)
}
