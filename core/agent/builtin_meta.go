package agent

// 内置工具的静态元信息（图标 / 默认描述 / 分组）。
//
// 放在基础层而非 tools 子包：前端设置页、tools 子包的提示词构建、
// agent 服务的 /api/agent/tools 都要用它，它又不依赖任何运行时状态。

// BuiltinToolDef 单个内置工具的静态定义（名称、图标、默认描述、分组）。
type BuiltinToolDef struct {
	Name    string `json:"name"`
	Icon    string `json:"icon"`
	Group   string `json:"group"`
	Default string `json:"default"` // 默认描述
}

// BuiltinToolMeta 返回全部内置工具的静态元信息（图标、默认描述、分组）。
// 顺序即设置页展示顺序。
func BuiltinToolMeta() []BuiltinToolDef {
	return []BuiltinToolDef{
		{Name: "read_file", Icon: "📄", Group: "文件操作", Default: "当用户需要查看某个文本/代码文件的内容时使用。传入文件绝对路径，可指定 limit 只读取前若干行。返回文件文本内容。"},
		{Name: "write_file", Icon: "✏️", Group: "文件操作", Default: "当用户需要创建新文件或覆盖写入内容时使用。传入文件绝对路径与要写入的完整 content（会覆盖原文件）。返回写入结果。"},
		{Name: "list_dir", Icon: "📂", Group: "文件操作", Default: "当用户想了解某个目录里有哪些文件或子目录时使用。传入目录路径，省略则默认当前工作目录。返回该目录下的文件/文件夹列表。"},
		{Name: "make_dir", Icon: "📁", Group: "文件操作", Default: "当用户需要新建目录时使用，支持一次创建多级目录（默认 all=true 递归创建）。传入目标目录路径。返回创建结果。"},
		{Name: "remove_dir", Icon: "🗑️", Group: "文件操作", Default: "当用户要删除目录时使用。默认仅删除空目录（安全）；若目录非空且确认可删，请传入 all=true 递归删除全部内容。传入目录路径。"},
		{Name: "remove_file", Icon: "❌", Group: "文件操作", Default: "当用户要删除某个具体的文件时使用（注意：不会删除目录）。传入文件绝对路径。返回删除结果。"},
		{Name: "rename_path", Icon: "🔀", Group: "文件操作", Default: "当用户要重命名文件/目录，或把文件/目录移动到新位置时使用。传入 src（原路径）与 dst（新路径）。返回移动/重命名结果。"},
		{Name: "web_search", Icon: "🔍", Group: "网页搜索", Default: "当用户的问题需要最新信息、实时数据、或你不确定答案时才使用。传入 query 关键词，可指定 limit 返回条数。返回相关网页的标题、摘要与链接。"},
		{Name: "system_info", Icon: "💻", Group: "系统信息", Default: "当用户想了解运行本程序的这台机器信息时使用：操作系统、CPU、内存、主机名、当前时间、当前工作目录等。无需参数。"},
		{Name: "get_time", Icon: "⏰", Group: "常用工具", Default: "当用户需要知道当前日期/时间，或指定时区（如 Asia/Shanghai）的时间时使用。返回格式化的本地时间。"},
		{Name: "calc", Icon: "🧮", Group: "常用工具", Default: "当用户需要计算一个算术表达式时使用，仅支持数字与 + - * / 和括号 ()。传入 expr，例如 \"1+2*3\"。返回计算结果。"},
		{Name: "run_command", Icon: "⌨️", Group: "常用工具", Default: "当用户需要执行一条本机 shell 命令（如 git、npm、系统命令）并获取其输出时使用。传入 command 字符串。⚠️ 会真实执行命令，请仅在确定安全时使用。"},
		{Name: "db_schema", Icon: "🗄️", Group: "数据库连接分析", Default: "返回用户在「插件/数据库连接」中已同步配置的表结构（库/表/字段名/类型/可空/默认值/注释），把数据库结构交给大模型用于生成 SQL 与数据分析。注意：本工具只返回你已同步的表，不会实时读取数据库全部表；大模型应严格只基于返回的这些表与字段编写 SQL，不得臆测或使用未列出的表/字段。返回的字段注释会叠加用户维护的表级与字段级语义。connId 与 database 为必填；tables 留空则返回该库下所有已同步的表。connId 可省略，省略时自动使用当前激活的分析连接。"},
		{Name: "db_query", Icon: "🔎", Group: "数据库连接分析", Default: "在已配置的连接上执行只读 SELECT 查询并返回结果（限制行数），用于采样数据、核对数值或验证假设。支持 MySQL、PostgreSQL、Oracle。必须以 SELECT 或 WITH 开头，禁止 INSERT/UPDATE/DELETE/DDL 等任何写操作；connId 可省略（省略时用当前激活连接），但 database 与 sql 必填。结果过大时请补充 WHERE/LIMIT 条件。"},
		{Name: "export_table", Icon: "📊", Group: "数据导出", Default: "把表格数据导出为文件，便于用户用 Excel 打开或进一步处理。当你的回答中包含数据表格（尤其是数据库查询结果、统计汇总、明细清单）时，如果用户可能需要表格数据，就调用本工具把它导出成文件。data 支持三种写法：①直接复制你刚输出的 Markdown 表格（含 |---| 分隔行）；②CSV 文本；③JSON 数组（二维数组或对象数组）。format 默认 xlsx（Excel 可直接打开，数字列可求和排序），也可选 csv / html / md。导出后把文件路径告知用户。"},
	}
}

// GetBuiltinTools 返回全部内置工具的静态元信息（供前端设置页动态罗列，保证前后端一致）。
func (m *Manager) GetBuiltinTools() []BuiltinToolDef {
	return BuiltinToolMeta()
}
