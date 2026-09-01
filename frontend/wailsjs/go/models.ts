export namespace agent {
	
	export class ToolFlags {
	    enabled: Record<string, boolean>;
	    desc: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new ToolFlags(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.desc = source["desc"];
	    }
	}
	export class AgentConfig {
	    systemPrompt: string;
	    mode: string;
	    maxLoops: number;
	    contextLimit: number;
	    showThinking: boolean;
	    enablePolish: boolean;
	    enableChart: boolean;
	    temperature: number;
	    currentUserId: string;
	    tools: ToolFlags;
	    maxToolOutput: number;
	    maxFileRead: number;
	    maxTokens: number;
	
	    static createFrom(source: any = {}) {
	        return new AgentConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.systemPrompt = source["systemPrompt"];
	        this.mode = source["mode"];
	        this.maxLoops = source["maxLoops"];
	        this.contextLimit = source["contextLimit"];
	        this.showThinking = source["showThinking"];
	        this.enablePolish = source["enablePolish"];
	        this.enableChart = source["enableChart"];
	        this.temperature = source["temperature"];
	        this.currentUserId = source["currentUserId"];
	        this.tools = this.convertValues(source["tools"], ToolFlags);
	        this.maxToolOutput = source["maxToolOutput"];
	        this.maxFileRead = source["maxFileRead"];
	        this.maxTokens = source["maxTokens"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class AgentLog {
	    id: string;
	    time: string;
	    timestamp: number;
	    level: string;
	    category: string;
	    title: string;
	    detail: string;
	    durationMs: number;
	    userId: string;
	
	    static createFrom(source: any = {}) {
	        return new AgentLog(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.time = source["time"];
	        this.timestamp = source["timestamp"];
	        this.level = source["level"];
	        this.category = source["category"];
	        this.title = source["title"];
	        this.detail = source["detail"];
	        this.durationMs = source["durationMs"];
	        this.userId = source["userId"];
	    }
	}
	export class TokenUsage {
	    promptTokens: number;
	    completionTokens: number;
	    totalTokens: number;
	
	    static createFrom(source: any = {}) {
	        return new TokenUsage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.promptTokens = source["promptTokens"];
	        this.completionTokens = source["completionTokens"];
	        this.totalTokens = source["totalTokens"];
	    }
	}
	export class AgentStep {
	    type: string;
	    name: string;
	    server?: string;
	    input?: string;
	    output?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new AgentStep(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.name = source["name"];
	        this.server = source["server"];
	        this.input = source["input"];
	        this.output = source["output"];
	        this.error = source["error"];
	    }
	}
	export class AgentMsg {
	    id: string;
	    role: string;
	    content: string;
	    thinking?: string;
	    steps?: AgentStep[];
	    time: string;
	
	    static createFrom(source: any = {}) {
	        return new AgentMsg(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.role = source["role"];
	        this.content = source["content"];
	        this.thinking = source["thinking"];
	        this.steps = this.convertValues(source["steps"], AgentStep);
	        this.time = source["time"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class AgentSession {
	    id: string;
	    title: string;
	    createdAt: string;
	    updatedAt: string;
	    messages: AgentMsg[];
	    usage: TokenUsage;
	
	    static createFrom(source: any = {}) {
	        return new AgentSession(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.messages = this.convertValues(source["messages"], AgentMsg);
	        this.usage = this.convertValues(source["usage"], TokenUsage);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class AgentUser {
	    id: string;
	    name: string;
	    token: string;
	    roles: string[];
	
	    static createFrom(source: any = {}) {
	        return new AgentUser(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.token = source["token"];
	        this.roles = source["roles"];
	    }
	}
	export class MCPServer {
	    id: string;
	    name: string;
	    transport: string;
	    command: string;
	    args: string[];
	    env: Record<string, string>;
	    url: string;
	    headers: Record<string, string>;
	    enabled: boolean;
	    updatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new MCPServer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.transport = source["transport"];
	        this.command = source["command"];
	        this.args = source["args"];
	        this.env = source["env"];
	        this.url = source["url"];
	        this.headers = source["headers"];
	        this.enabled = source["enabled"];
	        this.updatedAt = source["updatedAt"];
	    }
	}
	export class AgentSkill {
	    id: string;
	    name: string;
	    description: string;
	    prompt: string;
	    enabled: boolean;
	    updatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new AgentSkill(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.prompt = source["prompt"];
	        this.enabled = source["enabled"];
	        this.updatedAt = source["updatedAt"];
	    }
	}
	export class AgentData {
	    config: AgentConfig;
	    skills: AgentSkill[];
	    servers: MCPServer[];
	    users: AgentUser[];
	    sessions: AgentSession[];
	    activeSession: string;
	    usage: TokenUsage;
	    messages: AgentMsg[];
	    logs: AgentLog[];
	
	    static createFrom(source: any = {}) {
	        return new AgentData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.config = this.convertValues(source["config"], AgentConfig);
	        this.skills = this.convertValues(source["skills"], AgentSkill);
	        this.servers = this.convertValues(source["servers"], MCPServer);
	        this.users = this.convertValues(source["users"], AgentUser);
	        this.sessions = this.convertValues(source["sessions"], AgentSession);
	        this.activeSession = source["activeSession"];
	        this.usage = this.convertValues(source["usage"], TokenUsage);
	        this.messages = this.convertValues(source["messages"], AgentMsg);
	        this.logs = this.convertValues(source["logs"], AgentLog);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	
	
	
	
	export class BuiltinToolDef {
	    name: string;
	    icon: string;
	    group: string;
	    default: string;
	
	    static createFrom(source: any = {}) {
	        return new BuiltinToolDef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.icon = source["icon"];
	        this.group = source["group"];
	        this.default = source["default"];
	    }
	}
	
	export class MCPTool {
	    name: string;
	    description: string;
	    inputSchema: number[];
	    server: string;
	    serverName: string;
	
	    static createFrom(source: any = {}) {
	        return new MCPTool(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.inputSchema = source["inputSchema"];
	        this.server = source["server"];
	        this.serverName = source["serverName"];
	    }
	}
	export class QueryAgentLogsArgs {
	    keyword: string;
	    level: string;
	    category: string;
	    limit: number;
	
	    static createFrom(source: any = {}) {
	        return new QueryAgentLogsArgs(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.keyword = source["keyword"];
	        this.level = source["level"];
	        this.category = source["category"];
	        this.limit = source["limit"];
	    }
	}
	export class RunAgentArgs {
	    input: string;
	    baseUrl: string;
	    apiKey: string;
	    model: string;
	    timeoutSec: number;
	    maxTokens: number;
	
	    static createFrom(source: any = {}) {
	        return new RunAgentArgs(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.input = source["input"];
	        this.baseUrl = source["baseUrl"];
	        this.apiKey = source["apiKey"];
	        this.model = source["model"];
	        this.timeoutSec = source["timeoutSec"];
	        this.maxTokens = source["maxTokens"];
	    }
	}
	export class RunAgentResult {
	    content: string;
	    thinking: string;
	    steps: AgentStep[];
	    plan?: string;
	    error?: string;
	    usage: TokenUsage;
	
	    static createFrom(source: any = {}) {
	        return new RunAgentResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.content = source["content"];
	        this.thinking = source["thinking"];
	        this.steps = this.convertValues(source["steps"], AgentStep);
	        this.plan = source["plan"];
	        this.error = source["error"];
	        this.usage = this.convertValues(source["usage"], TokenUsage);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	

}

export namespace ai {
	
	export class ChatMessage {
	    role: string;
	    content: string;
	
	    static createFrom(source: any = {}) {
	        return new ChatMessage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.role = source["role"];
	        this.content = source["content"];
	    }
	}

}

export namespace capture {
	
	export class CaptureServerInfo {
	    running: boolean;
	    addr: string;
	    port: string;
	    url: string;
	    token: string;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new CaptureServerInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.addr = source["addr"];
	        this.port = source["port"];
	        this.url = source["url"];
	        this.token = source["token"];
	        this.count = source["count"];
	    }
	}
	export class CapturedRequest {
	    id: string;
	    capturedAt: string;
	    method: string;
	    url: string;
	    host: string;
	    path: string;
	    origin: string;
	    query: model.KV[];
	    headers: model.KV[];
	    bodyType: string;
	    body: string;
	    statusCode: number;
	    statusText: string;
	    durationMs: number;
	    respHeaders: Record<string, string>;
	    respBody: string;
	    respIsJson: boolean;
	    pageUrl: string;
	    matchedUrl: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new CapturedRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.capturedAt = source["capturedAt"];
	        this.method = source["method"];
	        this.url = source["url"];
	        this.host = source["host"];
	        this.path = source["path"];
	        this.origin = source["origin"];
	        this.query = this.convertValues(source["query"], model.KV);
	        this.headers = this.convertValues(source["headers"], model.KV);
	        this.bodyType = source["bodyType"];
	        this.body = source["body"];
	        this.statusCode = source["statusCode"];
	        this.statusText = source["statusText"];
	        this.durationMs = source["durationMs"];
	        this.respHeaders = source["respHeaders"];
	        this.respBody = source["respBody"];
	        this.respIsJson = source["respIsJson"];
	        this.pageUrl = source["pageUrl"];
	        this.matchedUrl = source["matchedUrl"];
	        this.error = source["error"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace crypto {
	
	export class Result {
	    ok: boolean;
	    output: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.output = source["output"];
	        this.error = source["error"];
	    }
	}

}

export namespace frontend {
	
	export class FileFilter {
	    DisplayName: string;
	    Pattern: string;
	
	    static createFrom(source: any = {}) {
	        return new FileFilter(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.DisplayName = source["DisplayName"];
	        this.Pattern = source["Pattern"];
	    }
	}
	export class OpenDialogOptions {
	    DefaultDirectory: string;
	    DefaultFilename: string;
	    Title: string;
	    Filters: FileFilter[];
	    ShowHiddenFiles: boolean;
	    CanCreateDirectories: boolean;
	    ResolvesAliases: boolean;
	    TreatPackagesAsDirectories: boolean;
	
	    static createFrom(source: any = {}) {
	        return new OpenDialogOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.DefaultDirectory = source["DefaultDirectory"];
	        this.DefaultFilename = source["DefaultFilename"];
	        this.Title = source["Title"];
	        this.Filters = this.convertValues(source["Filters"], FileFilter);
	        this.ShowHiddenFiles = source["ShowHiddenFiles"];
	        this.CanCreateDirectories = source["CanCreateDirectories"];
	        this.ResolvesAliases = source["ResolvesAliases"];
	        this.TreatPackagesAsDirectories = source["TreatPackagesAsDirectories"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SaveDialogOptions {
	    DefaultDirectory: string;
	    DefaultFilename: string;
	    Title: string;
	    Filters: FileFilter[];
	    ShowHiddenFiles: boolean;
	    CanCreateDirectories: boolean;
	    TreatPackagesAsDirectories: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SaveDialogOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.DefaultDirectory = source["DefaultDirectory"];
	        this.DefaultFilename = source["DefaultFilename"];
	        this.Title = source["Title"];
	        this.Filters = this.convertValues(source["Filters"], FileFilter);
	        this.ShowHiddenFiles = source["ShowHiddenFiles"];
	        this.CanCreateDirectories = source["CanCreateDirectories"];
	        this.TreatPackagesAsDirectories = source["TreatPackagesAsDirectories"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace main {
	
	export class CallAIArgs {
	    baseUrl: string;
	    apiKey: string;
	    model: string;
	    timeoutSec: number;
	    maxTokens: number;
	    messages: ai.ChatMessage[];
	
	    static createFrom(source: any = {}) {
	        return new CallAIArgs(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.baseUrl = source["baseUrl"];
	        this.apiKey = source["apiKey"];
	        this.model = source["model"];
	        this.timeoutSec = source["timeoutSec"];
	        this.maxTokens = source["maxTokens"];
	        this.messages = this.convertValues(source["messages"], ai.ChatMessage);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CheckUpdateResult {
	    current: string;
	    latest: string;
	    hasNew: boolean;
	    url: string;
	    notes: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new CheckUpdateResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.current = source["current"];
	        this.latest = source["latest"];
	        this.hasNew = source["hasNew"];
	        this.url = source["url"];
	        this.notes = source["notes"];
	        this.error = source["error"];
	    }
	}

}

export namespace model {
	
	export class ResponseData {
	    status: number;
	    statusText: string;
	    headers: Record<string, string>;
	    body: string;
	    durationMs: number;
	    size: number;
	    isJson: boolean;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new ResponseData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.statusText = source["statusText"];
	        this.headers = source["headers"];
	        this.body = source["body"];
	        this.durationMs = source["durationMs"];
	        this.size = source["size"];
	        this.isJson = source["isJson"];
	        this.error = source["error"];
	    }
	}
	export class Field {
	    name: string;
	    type: string;
	    required: boolean;
	    example: string;
	    description: string;
	    children?: Field[];
	
	    static createFrom(source: any = {}) {
	        return new Field(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	        this.required = source["required"];
	        this.example = source["example"];
	        this.description = source["description"];
	        this.children = this.convertValues(source["children"], Field);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class KV {
	    key: string;
	    value: string;
	    description: string;
	    enabled: boolean;
	    type: string;
	
	    static createFrom(source: any = {}) {
	        return new KV(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.value = source["value"];
	        this.description = source["description"];
	        this.enabled = source["enabled"];
	        this.type = source["type"];
	    }
	}
	export class ApiInfo {
	    id: string;
	    dirId: string;
	    name: string;
	    method: string;
	    url: string;
	    description: string;
	    contentType: string;
	    headers: KV[];
	    query: KV[];
	    bodyType: string;
	    body: string;
	    formItems: KV[];
	    reqFields: Field[];
	    respFields: Field[];
	    preScript: string;
	    postScript: string;
	    lastResponse?: ResponseData;
	    updatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new ApiInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.dirId = source["dirId"];
	        this.name = source["name"];
	        this.method = source["method"];
	        this.url = source["url"];
	        this.description = source["description"];
	        this.contentType = source["contentType"];
	        this.headers = this.convertValues(source["headers"], KV);
	        this.query = this.convertValues(source["query"], KV);
	        this.bodyType = source["bodyType"];
	        this.body = source["body"];
	        this.formItems = this.convertValues(source["formItems"], KV);
	        this.reqFields = this.convertValues(source["reqFields"], Field);
	        this.respFields = this.convertValues(source["respFields"], Field);
	        this.preScript = source["preScript"];
	        this.postScript = source["postScript"];
	        this.lastResponse = this.convertValues(source["lastResponse"], ResponseData);
	        this.updatedAt = source["updatedAt"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ClipItem {
	    id: string;
	    type: string;
	    text: string;
	    imagePath: string;
	    width: number;
	    height: number;
	    time: string;
	    timestamp: number;
	
	    static createFrom(source: any = {}) {
	        return new ClipItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.type = source["type"];
	        this.text = source["text"];
	        this.imagePath = source["imagePath"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.time = source["time"];
	        this.timestamp = source["timestamp"];
	    }
	}
	export class ClipData {
	    history: ClipItem[];
	
	    static createFrom(source: any = {}) {
	        return new ClipData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.history = this.convertValues(source["history"], ClipItem);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PluginConn {
	    id: string;
	    category: string;
	    name: string;
	    host: string;
	    port: number;
	    username: string;
	    password: string;
	    database: string;
	    dbType: string;
	    dbIndex: number;
	    encoding: string;
	    useTLS: boolean;
	    remark: string;
	    updatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new PluginConn(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.category = source["category"];
	        this.name = source["name"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.username = source["username"];
	        this.password = source["password"];
	        this.database = source["database"];
	        this.dbType = source["dbType"];
	        this.dbIndex = source["dbIndex"];
	        this.encoding = source["encoding"];
	        this.useTLS = source["useTLS"];
	        this.remark = source["remark"];
	        this.updatedAt = source["updatedAt"];
	    }
	}
	export class PluginsData {
	    connections: PluginConn[];
	
	    static createFrom(source: any = {}) {
	        return new PluginsData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connections = this.convertValues(source["connections"], PluginConn);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ClipSettings {
	    monitor: boolean;
	    maxItems: number;
	
	    static createFrom(source: any = {}) {
	        return new ClipSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.monitor = source["monitor"];
	        this.maxItems = source["maxItems"];
	    }
	}
	export class Settings {
	    aiBaseUrl: string;
	    aiKey: string;
	    aiModel: string;
	    timeoutSec: number;
	    clipboard: ClipSettings;
	    cloudURL: string;
	    cloudToken: string;
	    cloudUser: string;
	    version: string;
	    updateURL: string;
	    theme: string;
	    accent: string;
	    autoSync: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.aiBaseUrl = source["aiBaseUrl"];
	        this.aiKey = source["aiKey"];
	        this.aiModel = source["aiModel"];
	        this.timeoutSec = source["timeoutSec"];
	        this.clipboard = this.convertValues(source["clipboard"], ClipSettings);
	        this.cloudURL = source["cloudURL"];
	        this.cloudToken = source["cloudToken"];
	        this.cloudUser = source["cloudUser"];
	        this.version = source["version"];
	        this.updateURL = source["updateURL"];
	        this.theme = source["theme"];
	        this.accent = source["accent"];
	        this.autoSync = source["autoSync"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class AssertionResult {
	    description: string;
	    passed: boolean;
	    detail: string;
	
	    static createFrom(source: any = {}) {
	        return new AssertionResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.description = source["description"];
	        this.passed = source["passed"];
	        this.detail = source["detail"];
	    }
	}
	export class TestResult {
	    caseId: string;
	    caseName: string;
	    category: string;
	    passed: boolean;
	    status: number;
	    durationMs: number;
	    error: string;
	    responseBody: string;
	    assertionResults: AssertionResult[];
	
	    static createFrom(source: any = {}) {
	        return new TestResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.caseId = source["caseId"];
	        this.caseName = source["caseName"];
	        this.category = source["category"];
	        this.passed = source["passed"];
	        this.status = source["status"];
	        this.durationMs = source["durationMs"];
	        this.error = source["error"];
	        this.responseBody = source["responseBody"];
	        this.assertionResults = this.convertValues(source["assertionResults"], AssertionResult);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TestReport {
	    id: string;
	    planId: string;
	    planName: string;
	    createdAt: string;
	    total: number;
	    passed: number;
	    failed: number;
	    durationMs: number;
	    results: TestResult[];
	    summary: string;
	
	    static createFrom(source: any = {}) {
	        return new TestReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.planId = source["planId"];
	        this.planName = source["planName"];
	        this.createdAt = source["createdAt"];
	        this.total = source["total"];
	        this.passed = source["passed"];
	        this.failed = source["failed"];
	        this.durationMs = source["durationMs"];
	        this.results = this.convertValues(source["results"], TestResult);
	        this.summary = source["summary"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TestPlan {
	    id: string;
	    name: string;
	    caseIds: string[];
	    envId: string;
	    concurrency: number;
	    createdAt: string;
	    updatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new TestPlan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.caseIds = source["caseIds"];
	        this.envId = source["envId"];
	        this.concurrency = source["concurrency"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	    }
	}
	export class Assertion {
	    type: string;
	    target: string;
	    operator: string;
	    expected: string;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Assertion(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.target = source["target"];
	        this.operator = source["operator"];
	        this.expected = source["expected"];
	        this.enabled = source["enabled"];
	    }
	}
	export class TestCase {
	    id: string;
	    apiId: string;
	    apiName: string;
	    category: string;
	    name: string;
	    description: string;
	    method: string;
	    url: string;
	    headers: KV[];
	    query: KV[];
	    bodyType: string;
	    body: string;
	    formItems: KV[];
	    contentType: string;
	    assertions: Assertion[];
	    enabled: boolean;
	    createdAt: string;
	    dirId?: string;
	    dirName?: string;
	
	    static createFrom(source: any = {}) {
	        return new TestCase(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.apiId = source["apiId"];
	        this.apiName = source["apiName"];
	        this.category = source["category"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.method = source["method"];
	        this.url = source["url"];
	        this.headers = this.convertValues(source["headers"], KV);
	        this.query = this.convertValues(source["query"], KV);
	        this.bodyType = source["bodyType"];
	        this.body = source["body"];
	        this.formItems = this.convertValues(source["formItems"], KV);
	        this.contentType = source["contentType"];
	        this.assertions = this.convertValues(source["assertions"], Assertion);
	        this.enabled = source["enabled"];
	        this.createdAt = source["createdAt"];
	        this.dirId = source["dirId"];
	        this.dirName = source["dirName"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CommonParams {
	    headers: KV[];
	    query: KV[];
	
	    static createFrom(source: any = {}) {
	        return new CommonParams(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.headers = this.convertValues(source["headers"], KV);
	        this.query = this.convertValues(source["query"], KV);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class EnvVar {
	    key: string;
	    value: string;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new EnvVar(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.value = source["value"];
	        this.enabled = source["enabled"];
	    }
	}
	export class Environment {
	    id: string;
	    name: string;
	    vars: EnvVar[];
	
	    static createFrom(source: any = {}) {
	        return new Environment(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.vars = this.convertValues(source["vars"], EnvVar);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Directory {
	    id: string;
	    parentId: string;
	    name: string;
	    sort: number;
	
	    static createFrom(source: any = {}) {
	        return new Directory(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.parentId = source["parentId"];
	        this.name = source["name"];
	        this.sort = source["sort"];
	    }
	}
	export class Project {
	    id: string;
	    name: string;
	    dirs: Directory[];
	    apis: ApiInfo[];
	    environments: Environment[];
	    activeEnvId: string;
	    common: CommonParams;
	    updatedAt: string;
	    testCases?: TestCase[];
	    testPlans?: TestPlan[];
	    testReports?: TestReport[];
	
	    static createFrom(source: any = {}) {
	        return new Project(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.dirs = this.convertValues(source["dirs"], Directory);
	        this.apis = this.convertValues(source["apis"], ApiInfo);
	        this.environments = this.convertValues(source["environments"], Environment);
	        this.activeEnvId = source["activeEnvId"];
	        this.common = this.convertValues(source["common"], CommonParams);
	        this.updatedAt = source["updatedAt"];
	        this.testCases = this.convertValues(source["testCases"], TestCase);
	        this.testPlans = this.convertValues(source["testPlans"], TestPlan);
	        this.testReports = this.convertValues(source["testReports"], TestReport);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class AppData {
	    projects: Project[];
	    currentProjectId: string;
	    settings: Settings;
	    plugins: PluginsData;
	    clipboard: ClipData;
	
	    static createFrom(source: any = {}) {
	        return new AppData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projects = this.convertValues(source["projects"], Project);
	        this.currentProjectId = source["currentProjectId"];
	        this.settings = this.convertValues(source["settings"], Settings);
	        this.plugins = this.convertValues(source["plugins"], PluginsData);
	        this.clipboard = this.convertValues(source["clipboard"], ClipData);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	export class RequestSpec {
	    method: string;
	    url: string;
	    headers: KV[];
	    query: KV[];
	    bodyType: string;
	    body: string;
	    formItems: KV[];
	    timeoutSec: number;
	    env: KV[];
	    contentType: string;
	
	    static createFrom(source: any = {}) {
	        return new RequestSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.method = source["method"];
	        this.url = source["url"];
	        this.headers = this.convertValues(source["headers"], KV);
	        this.query = this.convertValues(source["query"], KV);
	        this.bodyType = source["bodyType"];
	        this.body = source["body"];
	        this.formItems = this.convertValues(source["formItems"], KV);
	        this.timeoutSec = source["timeoutSec"];
	        this.env = this.convertValues(source["env"], KV);
	        this.contentType = source["contentType"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	
	
	

}

export namespace plugins {
	
	export class DBExecReq {
	    database: string;
	    sql: string;
	
	    static createFrom(source: any = {}) {
	        return new DBExecReq(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.database = source["database"];
	        this.sql = source["sql"];
	    }
	}
	export class DBInfo {
	    ok: boolean;
	    error: string;
	    databases: string[];
	
	    static createFrom(source: any = {}) {
	        return new DBInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.error = source["error"];
	        this.databases = source["databases"];
	    }
	}
	export class DBQueryReq {
	    database: string;
	    sql: string;
	    limit: number;
	
	    static createFrom(source: any = {}) {
	        return new DBQueryReq(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.database = source["database"];
	        this.sql = source["sql"];
	        this.limit = source["limit"];
	    }
	}
	export class DBRow {
	    columns: string[];
	    rows: string[][];
	
	    static createFrom(source: any = {}) {
	        return new DBRow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.columns = source["columns"];
	        this.rows = source["rows"];
	    }
	}
	export class DBTable {
	    name: string;
	    rows: number;
	    engine: string;
	
	    static createFrom(source: any = {}) {
	        return new DBTable(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.rows = source["rows"];
	        this.engine = source["engine"];
	    }
	}
	export class ESIndex {
	    index: string;
	    docs: number;
	    health: string;
	
	    static createFrom(source: any = {}) {
	        return new ESIndex(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.docs = source["docs"];
	        this.health = source["health"];
	    }
	}
	export class FileInfo {
	    name: string;
	    path: string;
	    isDir: boolean;
	    size: number;
	    mode: string;
	    mtime: string;
	
	    static createFrom(source: any = {}) {
	        return new FileInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.isDir = source["isDir"];
	        this.size = source["size"];
	        this.mode = source["mode"];
	        this.mtime = source["mtime"];
	    }
	}
	export class PluginOpResult {
	    ok: boolean;
	    error: string;
	    info: string;
	
	    static createFrom(source: any = {}) {
	        return new PluginOpResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.error = source["error"];
	        this.info = source["info"];
	    }
	}
	export class RedisItem {
	    field: string;
	    value: string;
	
	    static createFrom(source: any = {}) {
	        return new RedisItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.field = source["field"];
	        this.value = source["value"];
	    }
	}
	export class RedisKey {
	    key: string;
	    type: string;
	
	    static createFrom(source: any = {}) {
	        return new RedisKey(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.type = source["type"];
	    }
	}
	export class RedisValue {
	    key: string;
	    type: string;
	    value: string;
	    items?: RedisItem[];
	
	    static createFrom(source: any = {}) {
	        return new RedisValue(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.type = source["type"];
	        this.value = source["value"];
	        this.items = this.convertValues(source["items"], RedisItem);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace share {
	
	export class ShareItemView {
	    token: string;
	    title: string;
	    hasPassword: boolean;
	    expireAt: number;
	
	    static createFrom(source: any = {}) {
	        return new ShareItemView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.token = source["token"];
	        this.title = source["title"];
	        this.hasPassword = source["hasPassword"];
	        this.expireAt = source["expireAt"];
	    }
	}
	export class ShareServerInfo {
	    running: boolean;
	    addr: string;
	    port: string;
	    url: string;
	    public: string;
	    host: string;
	
	    static createFrom(source: any = {}) {
	        return new ShareServerInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.addr = source["addr"];
	        this.port = source["port"];
	        this.url = source["url"];
	        this.public = source["public"];
	        this.host = source["host"];
	    }
	}

}

export namespace sniff {
	
	export class ErrorInfo {
	    type: string;
	    host: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new ErrorInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.host = source["host"];
	        this.message = source["message"];
	    }
	}
	export class Filter {
	    host: string;
	    excludeHosts: string[];
	    processName: string;
	    method: string;
	    pathKeyword: string;
	    onlyHttp: boolean;
	    protocols: string[];
	
	    static createFrom(source: any = {}) {
	        return new Filter(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.host = source["host"];
	        this.excludeHosts = source["excludeHosts"];
	        this.processName = source["processName"];
	        this.method = source["method"];
	        this.pathKeyword = source["pathKeyword"];
	        this.onlyHttp = source["onlyHttp"];
	        this.protocols = source["protocols"];
	    }
	}
	export class RewriteItem {
	    type: string;
	    action: string;
	    key: string;
	    value: string;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RewriteItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.action = source["action"];
	        this.key = source["key"];
	        this.value = source["value"];
	        this.enabled = source["enabled"];
	    }
	}
	export class HostRewrite {
	    id: string;
	    from: string;
	    to: string;
	    enabled: boolean;
	    desc: string;
	    scheme?: string;
	    replacements?: RewriteItem[];
	
	    static createFrom(source: any = {}) {
	        return new HostRewrite(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.from = source["from"];
	        this.to = source["to"];
	        this.enabled = source["enabled"];
	        this.desc = source["desc"];
	        this.scheme = source["scheme"];
	        this.replacements = this.convertValues(source["replacements"], RewriteItem);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class TrafficRecord {
	    id: string;
	    sessionId: string;
	    timestamp: string;
	    protocol: string;
	    decrypted: boolean;
	    method: string;
	    url: string;
	    host: string;
	    path: string;
	    query: model.KV[];
	    reqHeaders: model.KV[];
	    reqBody: string;
	    reqBodyType: string;
	    statusCode: number;
	    statusText: string;
	    respHeaders: model.KV[];
	    respBody: string;
	    respBodyType: string;
	    respContentType: string;
	    durationMs: number;
	    processName: string;
	    note: string;
	    error: string;
	    reqClipped?: boolean;
	    respClipped?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new TrafficRecord(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.sessionId = source["sessionId"];
	        this.timestamp = source["timestamp"];
	        this.protocol = source["protocol"];
	        this.decrypted = source["decrypted"];
	        this.method = source["method"];
	        this.url = source["url"];
	        this.host = source["host"];
	        this.path = source["path"];
	        this.query = this.convertValues(source["query"], model.KV);
	        this.reqHeaders = this.convertValues(source["reqHeaders"], model.KV);
	        this.reqBody = source["reqBody"];
	        this.reqBodyType = source["reqBodyType"];
	        this.statusCode = source["statusCode"];
	        this.statusText = source["statusText"];
	        this.respHeaders = this.convertValues(source["respHeaders"], model.KV);
	        this.respBody = source["respBody"];
	        this.respBodyType = source["respBodyType"];
	        this.respContentType = source["respContentType"];
	        this.durationMs = source["durationMs"];
	        this.processName = source["processName"];
	        this.note = source["note"];
	        this.error = source["error"];
	        this.reqClipped = source["reqClipped"];
	        this.respClipped = source["respClipped"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Session {
	    id: string;
	    name: string;
	    startedAt: string;
	    stoppedAt: string;
	    proxyAddr: string;
	    records: TrafficRecord[];
	    errors: ErrorInfo[];
	    recordCount?: number;
	
	    static createFrom(source: any = {}) {
	        return new Session(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.startedAt = source["startedAt"];
	        this.stoppedAt = source["stoppedAt"];
	        this.proxyAddr = source["proxyAddr"];
	        this.records = this.convertValues(source["records"], TrafficRecord);
	        this.errors = this.convertValues(source["errors"], ErrorInfo);
	        this.recordCount = source["recordCount"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Status {
	    running: boolean;
	    proxyAddr: string;
	    certURL: string;
	    localCertURL: string;
	    caInstalled: boolean;
	    caFingerprint: string;
	    systemProxy: boolean;
	    error: string;
	    activeSessionId: string;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.proxyAddr = source["proxyAddr"];
	        this.certURL = source["certURL"];
	        this.localCertURL = source["localCertURL"];
	        this.caInstalled = source["caInstalled"];
	        this.caFingerprint = source["caFingerprint"];
	        this.systemProxy = source["systemProxy"];
	        this.error = source["error"];
	        this.activeSessionId = source["activeSessionId"];
	    }
	}

}

export namespace store {
	
	export class Store {
	
	
	    static createFrom(source: any = {}) {
	        return new Store(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}

}

export namespace stress {
	
	export class StressConfig {
	    envId: string;
	    concurrency: number;
	    requests: number;
	    timeoutSec: number;
	
	    static createFrom(source: any = {}) {
	        return new StressConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.envId = source["envId"];
	        this.concurrency = source["concurrency"];
	        this.requests = source["requests"];
	        this.timeoutSec = source["timeoutSec"];
	    }
	}
	export class StressResult {
	    name: string;
	    method: string;
	    url: string;
	    total: number;
	    success: number;
	    failed: number;
	    statusDist: Record<string, number>;
	    errorDist: Record<string, number>;
	    minMs: number;
	    maxMs: number;
	    avgMs: number;
	    p50: number;
	    p90: number;
	    p95: number;
	    p99: number;
	
	    static createFrom(source: any = {}) {
	        return new StressResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.method = source["method"];
	        this.url = source["url"];
	        this.total = source["total"];
	        this.success = source["success"];
	        this.failed = source["failed"];
	        this.statusDist = source["statusDist"];
	        this.errorDist = source["errorDist"];
	        this.minMs = source["minMs"];
	        this.maxMs = source["maxMs"];
	        this.avgMs = source["avgMs"];
	        this.p50 = source["p50"];
	        this.p90 = source["p90"];
	        this.p95 = source["p95"];
	        this.p99 = source["p99"];
	    }
	}
	export class StressReport {
	    total: number;
	    success: number;
	    failed: number;
	    durationMs: number;
	    rps: number;
	    results: StressResult[];
	
	    static createFrom(source: any = {}) {
	        return new StressReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total = source["total"];
	        this.success = source["success"];
	        this.failed = source["failed"];
	        this.durationMs = source["durationMs"];
	        this.rps = source["rps"];
	        this.results = this.convertValues(source["results"], StressResult);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class StressTarget {
	    name: string;
	    method: string;
	    url: string;
	    headers: model.KV[];
	    query: model.KV[];
	    bodyType: string;
	    body: string;
	    formItems: model.KV[];
	    contentType: string;
	
	    static createFrom(source: any = {}) {
	        return new StressTarget(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.method = source["method"];
	        this.url = source["url"];
	        this.headers = this.convertValues(source["headers"], model.KV);
	        this.query = this.convertValues(source["query"], model.KV);
	        this.bodyType = source["bodyType"];
	        this.body = source["body"];
	        this.formItems = this.convertValues(source["formItems"], model.KV);
	        this.contentType = source["contentType"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace sync {
	
	export class ShareBackend {
	    url: string;
	    publicUrl: string;
	    token: string;
	    running: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ShareBackend(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.publicUrl = source["publicUrl"];
	        this.token = source["token"];
	        this.running = source["running"];
	    }
	}

}

