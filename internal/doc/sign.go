package doc

import "strings"

// 企业接口文档「加密与验签」章节的预置模板。
//
// 以「清单 + 内容」形式提供：每套模板都是一段简化 Markdown，包含签名规则、示例、
// 示例代码与验签错误码。前端下拉选择后一键填入章节，用户可再按实际算法修改。

// DocSignTemplate 一套验签说明模板。
type DocSignTemplate struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Desc    string `json:"desc"`
	Content string `json:"content"` // Markdown 正文（写入「加密与验签」章节）
}

// signDoc 按段落拼装模板正文（空段自动跳过，段间空一行）。
func signDoc(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			kept = append(kept, s)
		}
	}
	return strings.Join(kept, "\n\n") + "\n"
}

// signCodeTable 各方案通用的验签错误码表。
const signCodeTable = "#### 4. 验签错误码约定\n" +
	"验签类错误统一返回 **HTTP 200 + 业务码**，便于调用方按业务码分支处理：\n\n" +
	"| 业务码 | 含义 | 排查建议 |\n|---|---|---|\n" +
	"| 10001 | 缺少签名参数 | 检查 `X-Sign` / `sign` 是否传递 |\n" +
	"| 10002 | 签名不匹配 | 核对排序规则、空值是否剔除、密钥版本、是否多传或少传参数 |\n" +
	"| 10003 | 时间戳过期 | 客户端校时；与服务端偏差超过 5 分钟即拒绝 |\n" +
	"| 10004 | nonce 重复 | 同一 `X-Nonce` 5 分钟内只能使用一次（防重放） |\n" +
	"| 10005 | appId 无效或已禁用 | 确认应用状态、密钥是否已轮换 |"

// signNotesCommon 通用注意事项。
const signNotesCommon = "#### 5. 注意事项\n" +
	"- 参与签名的值必须与报文实际发送的值**完全一致**：金额统一「分」、时间统一 `yyyy-MM-dd HH:mm:ss`、布尔小写 `true`/`false`\n" +
	"- 数组统一约定为逗号分隔字符串（如 `ids=1,2,3`），避免各端序列化差异\n" +
	"- 空字符串与 `null` 一律视为「不参与签名」\n" +
	"- 密钥只保存在服务端或配置中心，禁止写入日志、前端源码与错误响应"

// ---------------- 方案 A：MD5 参数排序拼接 ----------------

const rulesMD5 = "#### 1. 签名规则\n" +
	"- **参与签名**：除 `sign`、`sign_type` 外的全部业务参数（含公共参数）；值为空的参数不参与签名\n" +
	"- **排序**：按参数名 ASCII 码升序（如 `appId` < `page` < `size` < `timestamp`）\n" +
	"- **拼接**：`key1=value1&key2=value2`，不做 URL 编码，值保持原始字符串（中文按 UTF-8）\n" +
	"- **加盐**：末尾拼接 `&key=appSecret`，`appSecret` 由平台分配\n" +
	"- **摘要**：`sign = MD5(待签串)`，输出 **32 位大写**十六进制\n" +
	"- **传递**：请求头 `X-App-Id`、`X-Timestamp`（毫秒）、`X-Nonce`、`X-Sign`；表单场景用 `sign` + `sign_type=MD5`"

const sampleMD5 = "#### 2. 示例\n" +
	"```text\n" +
	"业务参数：appId=A001, page=1, size=20, name=张三\n" +
	"待签串：  appId=A001&name=张三&page=1&size=20&key=9f2c***e1\n" +
	"sign：    4C1B7F0A9D2E5B83F6A0C71D9E4B2A08（32 位大写）\n" +
	"```"

const codeMD5 = "#### 3. 示例代码\n" +
	"```java\n" +
	"String stringA = params.entrySet().stream()\n" +
	"    .filter(e -> e.getValue() != null && !\"\".equals(e.getValue()))\n" +
	"    .filter(e -> !\"sign\".equals(e.getKey()) && !\"sign_type\".equals(e.getKey()))\n" +
	"    .sorted(Map.Entry.comparingByKey())\n" +
	"    .map(e -> e.getKey() + \"=\" + e.getValue())\n" +
	"    .collect(Collectors.joining(\"&\"));\n" +
	"String sign = DigestUtils.md5Hex(stringA + \"&key=\" + appSecret).toUpperCase();\n" +
	"```\n\n" +
	"```go\n" +
	"keys := make([]string, 0, len(params))\n" +
	"for k, v := range params {\n" +
	"    if v == \"\" || k == \"sign\" || k == \"sign_type\" {\n" +
	"        continue\n" +
	"    }\n" +
	"    keys = append(keys, k)\n" +
	"}\n" +
	"sort.Strings(keys)\n" +
	"parts := make([]string, 0, len(keys))\n" +
	"for _, k := range keys {\n" +
	"    parts = append(parts, k+\"=\"+params[k])\n" +
	"}\n" +
	"sum := md5.Sum([]byte(strings.Join(parts, \"&\") + \"&key=\" + appSecret))\n" +
	"sign := strings.ToUpper(hex.EncodeToString(sum[:]))\n" +
	"```\n\n" +
	"```javascript\n" +
	"const stringA = Object.keys(params)\n" +
	"  .filter(k => k !== 'sign' && k !== 'sign_type')\n" +
	"  .filter(k => params[k] !== '' && params[k] != null)\n" +
	"  .sort().map(k => k + '=' + params[k]).join('&')\n" +
	"const sign = md5(stringA + '&key=' + appSecret).toUpperCase() // md5 需引入 js-md5\n" +
	"```"

var templateMD5Sorted = signDoc(rulesMD5, sampleMD5, codeMD5, signCodeTable, signNotesCommon)

// ---------------- 方案 B：RSA2（SHA256withRSA） ----------------

const rulesRSA2 = "#### 1. 签名规则\n" +
	"- **参与签名**：全部业务参数（剔除 `sign`、`sign_type` 与空值），按参数名 ASCII 升序拼接为 `k1=v1&k2=v2`\n" +
	"- **签名**：用**应用私钥**对拼接串做 `SHA256withRSA` 签名，结果 `Base64` 编码（不加 `&key=` 后缀）\n" +
	"- **验签**：平台用**应用公钥**验签；平台异步通知由平台私钥签名，调用方用平台公钥验签\n" +
	"- **传递**：请求参数 `sign=<URLEncode(Base64)>` + `sign_type=RSA2`，或请求头 `X-Sign`\n" +
	"- **密钥**：RSA 2048 位以上；私钥仅保存在服务端 / 调用方安全存储，公钥可公开"

const sampleRSA2 = "#### 2. 示例\n" +
	"```text\n" +
	"待签串：app_id=2021000&biz_content={\"amount\":100,\"outNo\":\"A001\"}&charset=utf-8&timestamp=2026-10-30 10:00:00\n" +
	"sign：  T3vK9pQ...（Base64，约 344 字符）\n" +
	"```"

const codeRSA2 = "#### 3. 示例代码\n" +
	"```java\n" +
	"Signature s = Signature.getInstance(\"SHA256WithRSA\");\n" +
	"s.initSign(privateKey);                       // PKCS8 私钥\n" +
	"s.update(stringA.getBytes(StandardCharsets.UTF_8));\n" +
	"String sign = Base64.getEncoder().encodeToString(s.sign());\n" +
	"\n" +
	"// 验签\n" +
	"Signature v = Signature.getInstance(\"SHA256WithRSA\");\n" +
	"v.initVerify(publicKey);\n" +
	"v.update(stringA.getBytes(StandardCharsets.UTF_8));\n" +
	"boolean ok = v.verify(Base64.getDecoder().decode(sign));\n" +
	"```\n\n" +
	"```go\n" +
	"h := sha256.Sum256([]byte(stringA))\n" +
	"sig, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, h[:])\n" +
	"if err != nil {\n" +
	"    return err\n" +
	"}\n" +
	"sign := base64.StdEncoding.EncodeToString(sig)\n" +
	"```"

const notesRSA2 = "#### 5. 注意事项\n" +
	"- 签名原文必须与报文**逐字节一致**：`biz_content` 这类 JSON 参数直接取报文原字符串，不要重新序列化\n" +
	"- `sign` 参与 URL 传输要 URL 编码；回调验签需去掉 `sign`、`sign_type` 后重新拼接\n" +
	"- 密钥轮换需保留旧公钥一个版本，避免切换期验签失败（业务码 10005）\n" +
	"- 金额、时间等字段的格式化规则与签名参与规则必须写进文档，避免各方实现不一致"

var templateRSA2 = signDoc(rulesRSA2, sampleRSA2, codeRSA2, signCodeTable, notesRSA2)

// ---------------- 方案 C：HMAC-SHA256 规范化请求 ----------------

const rulesHMAC = "#### 1. 签名规则\n" +
	"- **规范化请求**：`HTTPMethod` + `\\n` + `CanonicalURI` + `\\n` + `CanonicalQuery` + `\\n` + `CanonicalHeaders` + `\\n` + `SHA256Hex(Body)`\n" +
	"  - `CanonicalQuery`：参数按 key 升序，`k=v` 用 `&` 连接，值做 URL 编码\n" +
	"  - `CanonicalHeaders`：仅签约定义的头（如 `content-type`、`x-date`），key 小写，`k:v\\n` 结尾\n" +
	"- **待签串**：`HMAC-SHA256` + `\\n` + `X-Timestamp` + `\\n` + `X-Nonce` + `\\n` + `SHA256Hex(CanonicalRequest)`\n" +
	"- **签名**：`Signature = Hex(HMAC-SHA256(SecretKey, 待签串))`\n" +
	"- **传递**：`Authorization: HMAC-SHA256 {appId}:{signature}`，配套 `X-Date`、`X-Nonce`"

const sampleHMAC = "#### 2. 示例\n" +
	"```text\n" +
	"POST /user-center/users?page=1&size=20\n" +
	"canonical    = \"POST\\n/user-center/users\\npage=1&size=20\\ncontent-type:application/json\\nx-date:2026-10-30T10:00:00Z\\n\" + SHA256Hex(body)\n" +
	"stringToSign = \"HMAC-SHA256\\n1730000000000\\n8f2c1e\\n\" + SHA256Hex(canonical)\n" +
	"signature    = 9C1B7F...（十六进制）\n" +
	"```"

const codeHMAC = "#### 3. 示例代码\n" +
	"```java\n" +
	"Mac mac = Mac.getInstance(\"HmacSHA256\");\n" +
	"mac.init(new SecretKeySpec(secretKey.getBytes(StandardCharsets.UTF_8), \"HmacSHA256\"));\n" +
	"String signature = HexFormat.of().withUpperCase()\n" +
	"    .formatHex(mac.doFinal(stringToSign.getBytes(StandardCharsets.UTF_8)));\n" +
	"```\n\n" +
	"```go\n" +
	"m := hmac.New(sha256.New, []byte(secretKey))\n" +
	"m.Write([]byte(stringToSign))\n" +
	"signature := strings.ToUpper(hex.EncodeToString(m.Sum(nil)))\n" +
	"```\n\n" +
	"```javascript\n" +
	"// 需引入 crypto-js：body 摘要用 SHA256，再算 HMAC\n" +
	"const canonical = [method, path, sortedQuery, canonicalHeaders, sha256Hex(body)].join('\\n')\n" +
	"const stringToSign = ['HMAC-SHA256', timestamp, nonce, sha256Hex(canonical)].join('\\n')\n" +
	"const signature = CryptoJS.HmacSHA256(stringToSign, secretKey).toString().toUpperCase()\n" +
	"```"

const notesHMAC = "#### 5. 注意事项\n" +
	"- 空 body 也要计算摘要：`SHA256Hex(\"\")` = `e3b0c442...b855`，不能省略\n" +
	"- 参与签名的 header 集合要在文档里写死，避免各端「多签 / 少签」\n" +
	"- 网关转发不要改写 query 顺序与大小写，否则验签失败\n" +
	"- `X-Nonce` 服务端缓存 5 分钟；生产环境建议开启 nonce 去重（错误码 10004）"

var templateHMAC = signDoc(rulesHMAC, sampleHMAC, codeHMAC, signCodeTable, notesHMAC)

// ---------------- 方案 D：摘要 + 二次拼接（时间戳 / nonce） ----------------

const rulesDigestNonce = "#### 1. 签名规则\n" +
	"- **一次摘要**：业务参数（剔除 `sign` 与空值）按 ASCII 升序拼成 `stringA`，`digest = MD5(stringA)`（32 位小写）\n" +
	"- **二次拼接**：`sign = MD5(digest + appSecret + X-Timestamp + X-Nonce)`，输出 **32 位大写**\n" +
	"- **防重放**：`X-Timestamp`（毫秒）与服务端偏差超过 5 分钟即拒绝；`X-Nonce` 5 分钟内不可重复\n" +
	"- **传递**：`X-App-Id`、`X-Timestamp`、`X-Nonce`、`X-Sign`"

const sampleDigestNonce = "#### 2. 示例\n" +
	"```text\n" +
	"stringA = appId=A001&page=1&size=20\n" +
	"digest  = 5F2A9B...（MD5 of stringA，小写）\n" +
	"sign    = MD5(5F2A9B... + 9f2c***e1 + 1730000000000 + 8f2c1e) = A71C...（大写）\n" +
	"```"

const codeDigestNonce = "#### 3. 示例代码\n" +
	"```go\n" +
	"sum1 := md5.Sum([]byte(stringA))\n" +
	"digest := hex.EncodeToString(sum1[:])\n" +
	"sum2 := md5.Sum([]byte(digest + appSecret + timestamp + nonce))\n" +
	"sign := strings.ToUpper(hex.EncodeToString(sum2[:]))\n" +
	"```\n\n" +
	"```javascript\n" +
	"const digest = md5(stringA)\n" +
	"const sign = md5(digest + appSecret + timestamp + nonce).toUpperCase()\n" +
	"```"

const notesDigestNonce = "#### 5. 注意事项\n" +
	"- 该模式本质是**共享密钥**：密钥泄露即可伪造签名，对外接口建议升级为 RSA2 或 HMAC-SHA256\n" +
	"- `X-Nonce` 建议「时间戳 + 随机数」16 位，服务端用 Redis 做 5 分钟去重\n" +
	"- 客户端与服务端必须同源校时（NTP），否则时间窗校验误伤正常请求"

var templateDigestNonce = signDoc(rulesDigestNonce, sampleDigestNonce, codeDigestNonce, signCodeTable, notesDigestNonce)

// ---------------- 方案 E：JWT（HS256 / RS256） ----------------

const rulesJWT = "#### 1. 鉴权规则\n" +
	"- **令牌格式**：`Authorization: Bearer <header>.<payload>.<signature>`（三段 Base64Url）\n" +
	"- **Header**：`{\"alg\":\"HS256\",\"typ\":\"JWT\"}`，对外开放建议 `RS256`（私钥签名 / 公钥验签）\n" +
	"- **Payload 建议字段**：`iss`（签发方）、`sub`（用户 / 系统标识）、`aud`（受众）、`exp`（过期时间，建议 ≤ 2 小时）、`iat`、`jti`（唯一 ID，用于吊销）\n" +
	"- **签名对象**：`base64url(header) + \".\" + base64url(payload)`\n" +
	"- **刷新与吊销**：过期前用 refresh_token 换新令牌；退出登录把 `jti` 写入黑名单"

const sampleJWT = "#### 2. 示例\n" +
	"```text\n" +
	"header  = {\"alg\":\"HS256\",\"typ\":\"JWT\"}\n" +
	"payload = {\"iss\":\"apitool\",\"sub\":\"A001\",\"aud\":\"open-api\",\"exp\":1730003600,\"iat\":1730000000,\"jti\":\"8f2c1e\"}\n" +
	"token   = eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJhcGl0b29sIi...（略）\n" +
	"```"

const codeJWT = "#### 3. 示例代码\n" +
	"```go\n" +
	"token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{\n" +
	"    \"iss\": \"apitool\", \"sub\": \"A001\", \"aud\": \"open-api\",\n" +
	"    \"exp\": time.Now().Add(2 * time.Hour).Unix(), \"jti\": util.GenID(),\n" +
	"})\n" +
	"signed, err := token.SignedString([]byte(secretKey))\n" +
	"```\n\n" +
	"```javascript\n" +
	"// 调用方只需携带令牌\n" +
	"fetch('/api/user/list', { headers: { Authorization: 'Bearer ' + token } })\n" +
	"```"

const authCodeTable = "#### 4. 鉴权错误码约定\n" +
	"| 业务码 | 含义 | 排查建议 |\n|---|---|---|\n" +
	"| 10001 | 缺少 Authorization 头 | 检查是否传入 Bearer 令牌 |\n" +
	"| 10002 | 令牌格式错误 | 是否缺少 `header.payload.signature` 三段 |\n" +
	"| 10003 | 令牌已过期 | 用 refresh_token 续期或重新登录 |\n" +
	"| 10004 | 令牌已吊销（jti 命中黑名单） | 重新登录获取新令牌 |\n" +
	"| 10005 | 签名或签发方不匹配 | 核对 alg、密钥 / 公钥、`iss` 与 `aud` |"

const notesJWT = "#### 5. 注意事项\n" +
	"- JWT 只解决**身份与有效期**，业务参数防篡改仍需配合方案 A / B / C 的参数签名\n" +
	"- payload 仅 Base64 编码、可被解码，禁止存放手机号、身份证等敏感信息\n" +
	"- 时钟偏差容忍建议 60 秒，`exp` 与 `nbf` 同时校验\n" +
	"- 令牌有效期越短越安全，建议配合 refresh_token 使用"

var templateJWT = signDoc(rulesJWT, sampleJWT, codeJWT, authCodeTable, notesJWT)

// ---------------- 方案 F：国密 SM2 + SM3 ----------------

const rulesSM2 = "#### 1. 签名规则\n" +
	"- **参与签名**：全部业务参数（剔除 `sign`、`sign_type` 与空值），按 ASCII 升序拼接为 `k1=v1&k2=v2`\n" +
	"- **摘要**：`SM3(待签串)`；**签名**：SM2 私钥签名，推荐 `DER` 编码（兼容性最好）\n" +
	"- **传递**：`X-Sign`（Base64）、`X-Sign-Alg: SM2-SM3`、`X-Cert-SN`（证书序列号，便于多证书轮换）\n" +
	"- **密钥**：SM2 密钥对由国密 CA 或平台统一签发；userId 缺省 `1234567812345678`"

const sampleSM2 = "#### 2. 示例\n" +
	"```text\n" +
	"待签串：appId=A001&page=1&size=20&timestamp=1730000000000\n" +
	"digest：SM3(待签串) = 7C2A...（64 位十六进制）\n" +
	"sign：  MEUCIQ...（SM2 DER 签名的 Base64）\n" +
	"```"

const codeSM2 = "#### 3. 示例代码\n" +
	"```java\n" +
	"// BouncyCastle：需注册 BouncyCastleProvider\n" +
	"SM2Signer signer = new SM2Signer();\n" +
	"signer.init(true, new ParametersWithID(\n" +
	"    new ECPrivateKeyParameters(privateKey, SM2Util.DOMAIN), SM2Util.ID.getBytes(StandardCharsets.UTF_8)));\n" +
	"byte[] data = stringA.getBytes(StandardCharsets.UTF_8);\n" +
	"signer.update(data, 0, data.length);\n" +
	"String sign = Base64.getEncoder().encodeToString(signer.generateSignature());\n" +
	"```\n\n" +
	"```go\n" +
	"// github.com/tjfoc/gmsm/sm2\n" +
	"digest := sm3.Sm3Sum([]byte(stringA))\n" +
	"r, s, err := privateKey.Sign(rand.Reader, digest, crypto.Sm3)\n" +
	"if err != nil {\n" +
	"    return err\n" +
	"}\n" +
	"sign := base64.StdEncoding.EncodeToString(append(r.Bytes(), s.Bytes()...))\n" +
	"```"

const notesSM2 = "#### 5. 注意事项\n" +
	"- 签名格式（DER / R||S）、字符编码、userId 三项必须与对端完全一致，否则验签必然失败\n" +
	"- 国密改造建议灰度并行，用 `X-Sign-Alg` 区分新老算法\n" +
	"- 客户端接入需内置国密库（Android / iOS / 小程序均有成熟 SDK）\n" +
	"- 证书到期前提前 30 天双证书并行，避免批量验签失败"

var templateSM2 = signDoc(rulesSM2, sampleSM2, codeSM2, signCodeTable, notesSM2)

// signTemplateList 预置验签模板清单（顺序即下拉顺序，首项为默认）。
func signTemplateList() []DocSignTemplate {
	return []DocSignTemplate{
		{Key: "md5-sorted", Label: "MD5 参数排序拼接",
			Desc: "排序拼接 → &key=appSecret → MD5 32 位大写（内部系统最常见）", Content: templateMD5Sorted},
		{Key: "rsa2", Label: "RSA2 / SHA256withRSA",
			Desc: "排序拼接 → 私钥签名 → Base64，公钥验签（开放平台 / 支付）", Content: templateRSA2},
		{Key: "hmac-sha256", Label: "HMAC-SHA256 规范化请求",
			Desc: "Method / Path / Query / Body 规范化 → HMAC-SHA256（云 API / 网关）", Content: templateHMAC},
		{Key: "digest-nonce", Label: "摘要 + 二次拼接（时间戳 / nonce）",
			Desc: "MD5(stringA) 再拼 appSecret + timestamp + nonce（内部防重放）", Content: templateDigestNonce},
		{Key: "jwt", Label: "JWT（HS256 / RS256）",
			Desc: "Header.Payload.Signature，Bearer 传递（统一鉴权 / SSO）", Content: templateJWT},
		{Key: "sm2", Label: "国密 SM2 + SM3",
			Desc: "SM3 摘要 → SM2 私钥签名 → Base64（政企 / 金融合规）", Content: templateSM2},
	}
}

// SignTemplateList 返回预置验签模板清单（供前端「填入验签模板」下拉使用）。
func SignTemplateList() []DocSignTemplate { return signTemplateList() }

// DefaultSignTemplate 返回默认验签模板正文（MD5 参数排序拼接）。
func DefaultSignTemplate() string { return templateMD5Sorted }

// SignTemplateByKey 按 key 取模板正文（供「填入默认模板」等场景）。
func SignTemplateByKey(key string) string {
	for _, t := range signTemplateList() {
		if t.Key == key {
			return t.Content
		}
	}
	return templateMD5Sorted
}
