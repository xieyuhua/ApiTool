package doc

import "strings"

// 企业接口文档「加密与验签」章节的预置模板。
//
// 目前内置 1 套（MD5 参数排序拼接），以列表形式提供，后续新增 RSA2 / HMAC-SHA256 / 国密时
// 只需往 signTemplateList 里追加条目，前端下拉即可选择，无需改动渲染逻辑。

// DocSignTemplate 一套验签说明模板。
type DocSignTemplate struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Desc    string `json:"desc"`
	Content string `json:"content"` // Markdown 正文（写入「加密与验签」章节）
}

// signTemplateList 预置验签模板清单。
func signTemplateList() []DocSignTemplate {
	return []DocSignTemplate{{
		Key:     "md5-sorted",
		Label:   "MD5 参数排序拼接",
		Desc:    "参数去空值 → ASCII 排序 → k=v& 拼接 → 末尾加 &key=appSecret → MD5 32 位大写",
		Content: md5SortedSignTemplate,
	}}
}

// SignTemplateList 返回预置验签模板清单（供前端「填入验签模板」下拉使用）。
func SignTemplateList() []DocSignTemplate { return signTemplateList() }

// DefaultSignTemplate 返回默认验签模板正文（MD5 参数排序拼接）。
func DefaultSignTemplate() string { return md5SortedSignTemplate }

// md5SortedSignTemplateRaw 用 § 占位反引号，避免与 Go 原始字符串冲突。
const md5SortedSignTemplateRaw = `#### 1. 签名规则
- **参与签名**：除 §sign§、§sign_type§ 外的全部业务参数（含公共参数）；值为空的参数不参与签名
- **排序**：按参数名 ASCII 码升序（如 §appId§ < §page§ < §size§ < §timestamp§）
- **拼接**：§key1=value1&key2=value2§，不做 URL 编码，值保持原始字符串（中文按 UTF-8）
- **加盐**：末尾拼接 §&key=appSecret§，§appSecret§ 由平台分配，禁止写入前端代码或日志
- **摘要**：§sign = MD5(待签串)§，输出 **32 位大写**十六进制
- **传递**：请求头 §X-App-Id§、§X-Timestamp§（毫秒）、§X-Nonce§（随机串）、§X-Sign§；
  表单 / 查询场景可用参数 §sign§ + §sign_type=MD5§

#### 2. 示例
§§§text
业务参数：appId=A001, page=1, size=20, name=张三
待签串：  appId=A001&name=张三&page=1&size=20&key=9f2c***e1
sign：    4C1B7F0A9D2E5B83F6A0C71D9E4B2A08（32 位大写）
§§§

#### 3. 示例代码
§§§java
// Java：参数排序拼接 + MD5 + 转大写
String stringA = params.entrySet().stream()
    .filter(e -> e.getValue() != null && !"".equals(e.getValue()))
    .filter(e -> !"sign".equals(e.getKey()) && !"sign_type".equals(e.getKey()))
    .sorted(Map.Entry.comparingByKey())
    .map(e -> e.getKey() + "=" + e.getValue())
    .collect(Collectors.joining("&"));
String sign = DigestUtils.md5Hex(stringA + "&key=" + appSecret).toUpperCase();
§§§

§§§go
// Go：排序键 → 拼接 → MD5 → 大写十六进制
keys := make([]string, 0, len(params))
for k, v := range params {
    if v == "" || k == "sign" || k == "sign_type" {
        continue
    }
    keys = append(keys, k)
}
sort.Strings(keys)
parts := make([]string, 0, len(keys))
for _, k := range keys {
    parts = append(parts, k+"="+params[k])
}
sum := md5.Sum([]byte(strings.Join(parts, "&") + "&key=" + appSecret))
sign := strings.ToUpper(hex.EncodeToString(sum[:]))
§§§

§§§javascript
// 前端：同样的排序与拼接（md5 需自行引入，如 js-md5）
const stringA = Object.keys(params)
  .filter(k => k !== 'sign' && k !== 'sign_type')
  .filter(k => params[k] !== '' && params[k] != null)
  .sort()
  .map(k => k + '=' + params[k])
  .join('&')
const sign = md5(stringA + '&key=' + appSecret).toUpperCase()
§§§

#### 4. 验签错误码约定
验签类错误统一返回 **HTTP 200 + 业务码**，便于调用方按业务码分支处理：

| 业务码 | 含义 | 排查建议 |
|---|---|---|
| 10001 | 缺少签名参数 | 检查 §X-Sign§ / §sign§ 是否传递 |
| 10002 | 签名不匹配 | 核对排序规则、空值是否剔除、密钥版本、是否多传或少传参数 |
| 10003 | 时间戳过期 | 客户端校时；与服务端偏差超过 5 分钟即拒绝 |
| 10004 | nonce 重复 | 同一 §X-Nonce§ 5 分钟内只能使用一次（防重放） |
| 10005 | appId 无效或已禁用 | 确认应用状态、密钥是否已轮换 |

#### 5. 注意事项
- 参数值必须与报文实际发送的值**完全一致**：数字不要做千分位、金额统一「分」、时间统一 §yyyy-MM-dd HH:mm:ss§
- 数组参数统一约定为逗号分隔字符串（如 §ids=1,2,3§），避免各端序列化差异
- 布尔值统一小写 §true§ / §false§；空字符串与 §null§ 一样视为「不参与签名」
- 验签失败一律返回 HTTP 200 + 业务码，便于调用方按业务码分支处理
`

// md5SortedSignTemplate 还原反引号后的模板正文。
var md5SortedSignTemplate = strings.ReplaceAll(md5SortedSignTemplateRaw, "§", "`")
