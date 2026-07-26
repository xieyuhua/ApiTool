export namespace main {
	
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
	
	    static createFrom(source: any = {}) {
	        return new KV(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.value = source["value"];
	        this.description = source["description"];
	        this.enabled = source["enabled"];
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
	export class Settings {
	    aiBaseUrl: string;
	    aiKey: string;
	    aiModel: string;
	    timeoutSec: number;
	    cloudURL: string;
	    cloudToken: string;
	    cloudUser: string;
	    version: string;
	    updateURL: string;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.aiBaseUrl = source["aiBaseUrl"];
	        this.aiKey = source["aiKey"];
	        this.aiModel = source["aiModel"];
	        this.timeoutSec = source["timeoutSec"];
	        this.cloudURL = source["cloudURL"];
	        this.cloudToken = source["cloudToken"];
	        this.cloudUser = source["cloudUser"];
	        this.version = source["version"];
	        this.updateURL = source["updateURL"];
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
	    updatedAt: string;
	
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
	export class AppData {
	    projects: Project[];
	    currentProjectId: string;
	    settings: Settings;
	
	    static createFrom(source: any = {}) {
	        return new AppData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projects = this.convertValues(source["projects"], Project);
	        this.currentProjectId = source["currentProjectId"];
	        this.settings = this.convertValues(source["settings"], Settings);
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
	
	
	export class ShareBackend {
	    url: string;
	    token: string;
	    running: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ShareBackend(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.token = source["token"];
	        this.running = source["running"];
	    }
	}
	export class ShareInfo {
	    token: string;
	    title: string;
	    hasPassword: boolean;
	    expireAt: number;
	    createdAt: number;
	    link: string;
	
	    static createFrom(source: any = {}) {
	        return new ShareInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.token = source["token"];
	        this.title = source["title"];
	        this.hasPassword = source["hasPassword"];
	        this.expireAt = source["expireAt"];
	        this.createdAt = source["createdAt"];
	        this.link = source["link"];
	    }
	}

}

