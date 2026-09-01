// Package plugins 实现「外部连接管理」插件：Redis / Elasticsearch / SSH(含实时终端) /
// SFTP / FTP / 数据库(MySQL/PostgreSQL) 的最小协议客户端与连接池，以及对外的
// 操作接口（连接测试、列举、读写、下载等）。
//
// 该包不再持有 *App，仅通过 bus.Bus 与 Wails 运行时交互（事件推送 / 保存对话框 /
// 剪贴板），可被独立编译与单测。
package plugins

import (
	"apitool/internal/bus"
	"apitool/internal/model"
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/textproto"
	"os"
	"strconv"
	"strings"
	"time"

	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/crypto/ssh"
	"golang.org/x/text/encoding/simplifiedchinese"
)

// ===================== 插件管理：操作结果类型 =====================

// PluginOpResult 通用操作结果
type PluginOpResult struct {
	Ok    bool   `json:"ok"`
	Error string `json:"error"`
	Info  string `json:"info"`
}

// RedisKey 一个 Redis 键及其类型
type RedisKey struct {
	Key  string `json:"key"`
	Type string `json:"type"`
}

// RedisValue 一个 Redis 键的值（按类型返回）
type RedisValue struct {
	Key   string      `json:"key"`
	Type  string      `json:"type"`
	Value string      `json:"value"` // 字符串 / 数字等标量值
	Items []RedisItem `json:"items,omitempty"`
}

// RedisItem 哈希/集合等的字段-值对
type RedisItem struct {
	Field string `json:"field"`
	Value string `json:"value"`
}

// ESIndex 一个 ES 索引概览
type ESIndex struct {
	Index  string `json:"index"`
	Docs   int64  `json:"docs"`
	Health string `json:"health"`
}

// FileInfo 远程文件/目录
type FileInfo struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size"`
	Mode  string `json:"mode"`
	MTime string `json:"mtime"`
}

// DBInfo 数据库连接信息
type DBInfo struct {
	Ok        bool     `json:"ok"`
	Error     string   `json:"error"`
	Databases []string `json:"databases"`
}

// DBTable 一张表
type DBTable struct {
	Name   string `json:"name"`
	Rows   int64  `json:"rows"`
	Engine string `json:"engine"`
}

// DBColumn 字段定义（用于表结构分析）
type DBColumn struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable string `json:"nullable"` // YES / NO
	Default  string `json:"default"`
	Comment  string `json:"comment"`
}

// DBSchema 一张表的完整结构（含字段列表），用于同步给大模型分析
type DBSchema struct {
	Database string      `json:"database"`
	Table    string      `json:"table"`
	Rows     int64       `json:"rows"`
	Columns  []DBColumn  `json:"columns"`
}

// DBRow 查询结果
type DBRow struct {
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
}

func portOrDefault(p, d int) int {
	if p <= 0 {
		return d
	}
	return p
}

func opErr(e error) PluginOpResult {
	return PluginOpResult{Ok: false, Error: e.Error()}
}

// ===================== 连接池：按连接指纹复用，避免每次操作都重新建连 =====================

type pooledConn struct {
	val    interface{}
	close  func()
	mu     sync.Mutex
	expire time.Time
}

var (
	poolMu   sync.Mutex
	connPool = map[string]*pooledConn{}
)

const connPoolTTL = 90 * time.Second

// connKey 用连接的关键字段生成指纹；任一字段变化（如改密码）即视为新连接。
func connKey(c model.PluginConn) string {
	return fmt.Sprintf("%s|%s|%d|%s|%s|%s|%s", c.Category, c.Host, c.Port, c.Username, c.Password, c.DbType, c.Database)
}

// withConn 取/建与 key 关联的连接，并串行执行 fn（同一连接同一时刻仅一个操作在跑）。
func withConn(key string, factory func() (interface{}, func(), error), fn func(interface{}) error) error {
	poolMu.Lock()
	p := connPool[key]
	if p != nil && time.Now().After(p.expire) {
		go p.close()
		delete(connPool, key)
		p = nil
	}
	if p == nil {
		v, closeFn, err := factory()
		if err != nil {
			poolMu.Unlock()
			return err
		}
		p = &pooledConn{val: v, close: closeFn, expire: time.Now().Add(connPoolTTL)}
		connPool[key] = p
	}
	p.expire = time.Now().Add(connPoolTTL)
	poolMu.Unlock()

	p.mu.Lock()
	defer p.mu.Unlock()
	return fn(p.val)
}

// keepAlive 清除连接的读写超时，使其在连接池生命周期内可长期复用。
func keepAlive(c net.Conn) {
	_ = c.SetDeadline(time.Time{})
}

// ===================== 连接测试 =====================

// PluginTest 按分类对连接做最小连通性/登录校验
func PluginTest(conn model.PluginConn) PluginOpResult {
	switch conn.Category {
	case "redis":
		// dialRedis 已完成 AUTH，并根据 conn.DbIndex 自动 SELECT（默认 DB 0）
		rc, err := dialRedis(conn)
		if err != nil {
			return opErr(err)
		}
		defer rc.Close()
		if err := rc.cmd("PING"); err != nil {
			return opErr(err)
		}
		info := "Redis 连接成功"
		if conn.DbIndex != 0 {
			info += fmt.Sprintf("（DB %d）", conn.DbIndex)
		}
		return PluginOpResult{Ok: true, Info: info}
	case "es":
		body, err := esRequest(conn, "GET", "/", "")
		if err != nil {
			return opErr(err)
		}
		var m map[string]interface{}
		_ = json.Unmarshal([]byte(body), &m)
		ver := ""
		if v, ok := m["version"].(map[string]interface{}); ok {
			ver, _ = v["number"].(string)
		}
		return PluginOpResult{Ok: true, Info: "ES 连接成功，版本 " + ver}
	case "ssh":
		client, err := sshDial(conn)
		if err != nil {
			return opErr(err)
		}
		defer client.Close()
		return PluginOpResult{Ok: true, Info: "SSH 连接成功"}
	case "sftp":
		client, sc, err := openSFTP(conn)
		if err != nil {
			return opErr(err)
		}
		defer client.Close()
		defer sc.Close()
		return PluginOpResult{Ok: true, Info: "SFTP 连接成功"}
	case "ftp":
		fc, err := ftpDial(conn)
		if err != nil {
			return opErr(err)
		}
		defer fc.Close()
		return PluginOpResult{Ok: true, Info: "FTP 连接成功"}
	case "db":
		var n int
		err := withConn(connKey(conn), dbFactory(conn), func(v interface{}) error {
			s := v.(*dbSession)
			var res *DBRow
			var e error
			if s.dbType == "postgres" {
				res, e = s.pg.query("SELECT 1")
			} else {
				res, e = s.mysql.query("SELECT 1")
			}
			if e != nil {
				return e
			}
			n = len(res.Rows)
			return nil
		})
		if err != nil {
			return opErr(err)
		}
		return PluginOpResult{Ok: true, Info: fmt.Sprintf("数据库连接成功（SELECT 1 返回 %d 行）", n)}
	}
	return PluginOpResult{Ok: false, Error: "未知分类：" + conn.Category}
}

// ===================== Redis =====================

type respClient struct {
	conn net.Conn
	r    *bufio.Reader
}

func dialRedis(conn model.PluginConn) (*respClient, error) {
	addr := net.JoinHostPort(conn.Host, strconv.Itoa(portOrDefault(conn.Port, 6379)))
	c, err := net.DialTimeout("tcp", addr, 6*time.Second)
	if err != nil {
		return nil, err
	}
	c.SetDeadline(time.Now().Add(10 * time.Second))
	rc := &respClient{conn: c, r: bufio.NewReader(c)}
	if conn.Password != "" {
		if err := rc.cmd("AUTH", conn.Password); err != nil {
			c.Close()
			return nil, err
		}
	}
	// 选择非默认 DB（Redis 默认 DB 序号为 0）
	if conn.DbIndex != 0 {
		if err := rc.cmd("SELECT", strconv.Itoa(conn.DbIndex)); err != nil {
			c.Close()
			return nil, err
		}
	}
	return rc, nil
}

func (rc *respClient) Close() error { return rc.conn.Close() }

func (rc *respClient) send(args ...string) error {
	var b bytes.Buffer
	b.WriteString(fmt.Sprintf("*%d\r\n", len(args)))
	for _, a := range args {
		b.WriteString(fmt.Sprintf("$%d\r\n%s\r\n", len(a), a))
	}
	_, err := rc.conn.Write(b.Bytes())
	return err
}

// cmd 发送命令并读取单条回复
func (rc *respClient) cmd(args ...string) error {
	if err := rc.send(args...); err != nil {
		return err
	}
	_, err := rc.readReply()
	return err
}

// cmdReply 发送命令并返回回复
func (rc *respClient) cmdReply(args ...string) (interface{}, error) {
	if err := rc.send(args...); err != nil {
		return nil, err
	}
	return rc.readReply()
}

func (rc *respClient) readReply() (interface{}, error) {
	line, err := rc.r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimRight(line, "\r\n")
	if len(line) == 0 {
		return nil, fmt.Errorf("空回复")
	}
	t := line[0]
	rest := line[1:]
	switch t {
	case '+':
		return rest, nil
	case '-':
		return nil, fmt.Errorf("%s", rest)
	case ':':
		return rest, nil
	case '$':
		n, _ := strconv.Atoi(rest)
		if n == -1 {
			return nil, nil
		}
		buf := make([]byte, n)
		if _, err := io.ReadFull(rc.r, buf); err != nil {
			return nil, err
		}
		rc.r.Discard(2)
		return string(buf), nil
	case '*':
		n, _ := strconv.Atoi(rest)
		if n == -1 {
			return nil, nil
		}
		arr := make([]interface{}, n)
		for i := 0; i < n; i++ {
			v, err := rc.readReply()
			if err != nil {
				return nil, err
			}
			arr[i] = v
		}
		return arr, nil
	}
	return nil, fmt.Errorf("未知回复类型 %q", t)
}

func asString(v interface{}) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	default:
		return fmt.Sprintf("%v", x)
	}
}

func redisFactory(conn model.PluginConn) func() (interface{}, func(), error) {
	return func() (interface{}, func(), error) {
		rc, err := dialRedis(conn)
		if err != nil {
			return nil, nil, err
		}
		keepAlive(rc.conn)
		return rc, func() { rc.Close() }, nil
	}
}

// PluginRedisKeys 列举匹配模式的键
func PluginRedisKeys(conn model.PluginConn, pattern string, db int) ([]RedisKey, error) {
	if pattern == "" {
		pattern = "*"
	}
	var out []RedisKey
	err := withConn(connKey(conn), redisFactory(conn), func(v interface{}) error {
		rc := v.(*respClient)
		if e := rc.cmd("SELECT", strconv.Itoa(db)); e != nil {
			return e
		}
		rep, e := rc.cmdReply("KEYS", pattern)
		if e != nil {
			return e
		}
		arr, ok := rep.([]interface{})
		if !ok {
			out = []RedisKey{}
			return nil
		}
		keys := make([]RedisKey, 0, len(arr))
		for _, k := range arr {
			ks := asString(k)
			trep, _ := rc.cmdReply("TYPE", ks)
			keys = append(keys, RedisKey{Key: ks, Type: asString(trep)})
		}
		out = keys
		return nil
	})
	return out, err
}

// PluginRedisValue 获取键的值（按类型解析）
func PluginRedisValue(conn model.PluginConn, key string, db int) (RedisValue, error) {
	var res RedisValue
	err := withConn(connKey(conn), redisFactory(conn), func(v interface{}) error {
		rc := v.(*respClient)
		if e := rc.cmd("SELECT", strconv.Itoa(db)); e != nil {
			return e
		}
		trep, e := rc.cmdReply("TYPE", key)
		if e != nil {
			return e
		}
		t := asString(trep)
		res = RedisValue{Key: key, Type: t}
		switch t {
		case "string":
			v, _ := rc.cmdReply("GET", key)
			res.Value = asString(v)
		case "list":
			v, _ := rc.cmdReply("LRANGE", key, "0", "-1")
			res.Items = arrayToItems(v)
		case "set":
			v, _ := rc.cmdReply("SMEMBERS", key)
			res.Items = arrayToItems(v)
		case "zset":
			v, _ := rc.cmdReply("ZRANGE", key, "0", "-1", "WITHSCORES")
			if arr, ok := v.([]interface{}); ok {
				for i := 0; i+1 < len(arr); i += 2 {
					res.Items = append(res.Items, RedisItem{Field: asString(arr[i]), Value: asString(arr[i+1])})
				}
			}
		case "hash":
			v, _ := rc.cmdReply("HGETALL", key)
			res.Items = arrayToItems(v)
		case "none":
			res.Value = "（键不存在）"
		}
		return nil
	})
	return res, err
}

// PluginRedisDel 删除键
func PluginRedisDel(conn model.PluginConn, key string, db int) error {
	return withConn(connKey(conn), redisFactory(conn), func(v interface{}) error {
		rc := v.(*respClient)
		rc.cmd("SELECT", strconv.Itoa(db))
		return rc.cmd("DEL", key)
	})
}

// PluginRedisSet 设置字符串键（ttl>0 时设置过期秒数）
func PluginRedisSet(conn model.PluginConn, key, value string, ttl, db int) error {
	return withConn(connKey(conn), redisFactory(conn), func(v interface{}) error {
		rc := v.(*respClient)
		if e := rc.cmd("SELECT", strconv.Itoa(db)); e != nil {
			return e
		}
		if e := rc.cmd("SET", key, value); e != nil {
			return e
		}
		if ttl > 0 {
			return rc.cmd("EXPIRE", key, strconv.Itoa(ttl))
		}
		return nil
	})
}

// PluginRedisTTL 返回键过期秒数（-1 永不过期，-2 不存在）
func PluginRedisTTL(conn model.PluginConn, key string, db int) (int, error) {
	var ttl int
	err := withConn(connKey(conn), redisFactory(conn), func(v interface{}) error {
		rc := v.(*respClient)
		rc.cmd("SELECT", strconv.Itoa(db))
		rep, e := rc.cmdReply("TTL", key)
		if e != nil {
			return e
		}
		ttl, _ = strconv.Atoi(asString(rep))
		return nil
	})
	return ttl, err
}

func arrayToItems(v interface{}) []RedisItem {
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	items := make([]RedisItem, 0, len(arr))
	for i := 0; i+1 < len(arr); i += 2 {
		items = append(items, RedisItem{Field: asString(arr[i]), Value: asString(arr[i+1])})
	}
	return items
}

// ===================== Elasticsearch =====================

func esBaseURL(conn model.PluginConn) string {
	host := strings.TrimSpace(conn.Host)
	scheme := "http"
	if conn.UseTLS {
		scheme = "https"
	}
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		if i := strings.Index(host, "://"); i >= 0 {
			host = host[i+3:]
		}
	}
	if i := strings.Index(host, "/"); i >= 0 {
		host = host[:i]
	}
	u := scheme + "://" + host
	if conn.Port > 0 && !strings.Contains(host, ":") {
		u += ":" + strconv.Itoa(conn.Port)
	}
	return strings.TrimRight(u, "/")
}

func esRequest(conn model.PluginConn, method, path, body string) (string, error) {
	base := esBaseURL(conn)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	url := base + path
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if conn.Username != "" {
		req.SetBasicAuth(conn.Username, conn.Password)
	}
	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	client := &http.Client{Timeout: 15 * time.Second, Transport: tr}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return string(b), fmt.Errorf("ES 返回状态码 %d：%s", resp.StatusCode, truncate(string(b), 300))
	}
	return string(b), nil
}

func truncate(s string, n int) string {
	if n <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

// PluginESIndices 列出索引
func PluginESIndices(conn model.PluginConn) ([]ESIndex, error) {
	body, err := esRequest(conn, "GET", "/_cat/indices?format=json", "")
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Index     string `json:"index"`
		DocsCount string `json:"docs.count"`
		Health    string `json:"health"`
	}
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		return nil, err
	}
	out := make([]ESIndex, 0, len(raw))
	for _, r := range raw {
		n, _ := strconv.ParseInt(r.DocsCount, 10, 64)
		out = append(out, ESIndex{Index: r.Index, Docs: n, Health: r.Health})
	}
	return out, nil
}

// PluginESSearch 执行查询
func PluginESSearch(conn model.PluginConn, index, query string) (string, error) {
	if query == "" {
		query = `{"query":{"match_all":{}},"size":50}`
	}
	path := "/" + index + "/_search"
	return esRequest(conn, "POST", path, query)
}

// ===================== SSH / XShell =====================

func sshDial(conn model.PluginConn) (*ssh.Client, error) {
	cfg := &ssh.ClientConfig{
		User:            conn.Username,
		Auth:            []ssh.AuthMethod{ssh.Password(conn.Password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
	addr := net.JoinHostPort(conn.Host, strconv.Itoa(portOrDefault(conn.Port, 22)))
	return ssh.Dial("tcp", addr, cfg)
}

// sshSession 复用的 SSH 连接（用于一次性命令执行）
type sshSession struct {
	client *ssh.Client
}

func sshFactory(conn model.PluginConn) func() (interface{}, func(), error) {
	return func() (interface{}, func(), error) {
		client, err := sshDial(conn)
		if err != nil {
			return nil, nil, err
		}
		return &sshSession{client: client}, func() { client.Close() }, nil
	}
}

// PluginSSHExec 在远端执行命令（XShell 风格命令执行，复用同一 SSH 连接）
func PluginSSHExec(conn model.PluginConn, command string) (string, error) {
	var out string
	err := withConn(connKey(conn), sshFactory(conn), func(v interface{}) error {
		session, e := v.(*sshSession).client.NewSession()
		if e != nil {
			return e
		}
		defer session.Close()
		o, e := session.CombinedOutput(command)
		out = string(o)
		return e
	})
	return out, err
}

// ===================== SSH 实时终端（流式） =====================

type sshStream struct {
	client   *ssh.Client
	sess     *ssh.Session
	stdin    io.WriteCloser
	rows     int
	cols     int
	encoding string // 终端输出编码，用于解码（utf-8/gbk/gb18030）
}

var (
	sshMu      sync.Mutex
	sshStreams = map[string]*sshStream{}
)

type eventWriter struct {
	emit func(string)
}

func (w *eventWriter) Write(p []byte) (int, error) {
	w.emit(string(p))
	return len(p), nil
}

// PluginSSHOpen 建立带 PTY 的持久 SSH 会话，输出通过事件实时推送到前端
func PluginSSHOpen(b bus.Bus, conn model.PluginConn) (string, error) {
	client, err := sshDial(conn)
	if err != nil {
		return "", err
	}
	sess, err := client.NewSession()
	if err != nil {
		client.Close()
		return "", err
	}
	// 终端尺寸与编码（默认 40 行 120 列、UTF-8）
	const defRows, defCols = 40, 120
	encoding := strings.ToLower(conn.Encoding)
	if encoding == "" {
		encoding = "utf-8"
	}
	modes := ssh.TerminalModes{
		ssh.ECHO:          1, // 开启回显，由远端将输入回传给前端（与 XShell 一致）
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	// 申请伪终端，并设置 UTF-8 环境，避免中文乱码
	if err := sess.RequestPty("xterm-256color", defRows, defCols, modes); err != nil {
		sess.Close()
		client.Close()
		return "", err
	}
	_ = sess.Setenv("TERM", "xterm-256color")
	_ = sess.Setenv("LANG", "en_US.UTF-8")
	_ = sess.Setenv("LC_ALL", "en_US.UTF-8")
	stdin, err := sess.StdinPipe()
	if err != nil {
		sess.Close()
		client.Close()
		return "", err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		sess.Close()
		client.Close()
		return "", err
	}
	id := fmt.Sprintf("ssh_%d", time.Now().UnixNano())
	sess.Stderr = &eventWriter{emit: func(s string) { b.Emit("ssh:" + id + ":data", s) }}
	if err := sess.Shell(); err != nil {
		sess.Close()
		client.Close()
		return "", err
	}
	st := &sshStream{client: client, sess: sess, stdin: stdin, rows: defRows, cols: defCols, encoding: encoding}
	sshMu.Lock()
	sshStreams[id] = st
	sshMu.Unlock()
	go func() {
		buf := make([]byte, 4096)
		for {
			n, rerr := stdout.Read(buf)
			if n > 0 {
				// 按连接编码解码，确保中文等正确显示
				b.Emit("ssh:"+id+":data", decodeRemote(encoding, buf[:n]))
			}
			if rerr != nil {
				b.Emit("ssh:"+id+":close", "")
				return
			}
		}
	}()
	return id, nil
}

// decodeRemote 将远端字节按指定编码转换为字符串（utf-8/gbk/gb18030）
func decodeRemote(encoding string, b []byte) string {
	switch encoding {
	case "gbk":
		if out, err := simplifiedchinese.GBK.NewDecoder().Bytes(b); err == nil {
			return string(out)
		}
	case "gb18030":
		if out, err := simplifiedchinese.GB18030.NewDecoder().Bytes(b); err == nil {
			return string(out)
		}
	}
	return string(b)
}

// PluginSSHInput 向 SSH 会话标准输入写入数据（通常是命令 + 换行）
func PluginSSHInput(id string, data string) error {
	if st := getStream(id); st != nil {
		_, err := st.stdin.Write([]byte(data))
		return err
	}
	return fmt.Errorf("终端会话不存在或已关闭")
}

// PluginSSHClose 关闭 SSH 会话
func PluginSSHClose(id string) error {
	sshMu.Lock()
	st := sshStreams[id]
	if st != nil {
		delete(sshStreams, id)
	}
	sshMu.Unlock()
	if st == nil {
		return nil
	}
	st.sess.Close()
	st.client.Close()
	return nil
}

// PluginSSHResize 通知远端调整伪终端尺寸（行/列），适配前端容器大小
func PluginSSHResize(id string, rows, cols int) error {
	st := getStream(id)
	if st == nil {
		return fmt.Errorf("终端会话不存在或已关闭")
	}
	if rows <= 0 || cols <= 0 {
		return nil
	}
	st.rows, st.cols = rows, cols
	return st.sess.WindowChange(rows, cols)
}

func getStream(id string) *sshStream {
	sshMu.Lock()
	defer sshMu.Unlock()
	return sshStreams[id]
}

// ===================== SFTP =====================

type sftpHolder struct {
	client *ssh.Client
	sc     *sftpClient
}

func sftpFactory(conn model.PluginConn) func() (interface{}, func(), error) {
	return func() (interface{}, func(), error) {
		client, sc, err := openSFTP(conn)
		if err != nil {
			return nil, nil, err
		}
		return &sftpHolder{client: client, sc: sc}, func() { sc.Close(); client.Close() }, nil
	}
}

func joinRemotePath(base, name string) string {
	if base == "" || base == "/" {
		return "/" + name
	}
	return strings.TrimRight(base, "/") + "/" + name
}

// PluginSFTPList 列出远端目录
func PluginSFTPList(conn model.PluginConn, path string) ([]FileInfo, error) {
	var out []FileInfo
	err := withConn(connKey(conn), sftpFactory(conn), func(v interface{}) error {
		h := v.(*sftpHolder)
		if path == "" {
			path = "/"
		}
		files, e := h.sc.listDir(path)
		out = files
		return e
	})
	return out, err
}

// PluginSFTPRead 读取远端文件文本（最大 5MB）
func PluginSFTPRead(conn model.PluginConn, path string) (string, error) {
	var out string
	err := withConn(connKey(conn), sftpFactory(conn), func(v interface{}) error {
		var e error
		out, e = v.(*sftpHolder).sc.readFile(path)
		return e
	})
	return out, err
}

// PluginSFTPWrite 写入远端文件
func PluginSFTPWrite(conn model.PluginConn, path, content string) error {
	return withConn(connKey(conn), sftpFactory(conn), func(v interface{}) error {
		return v.(*sftpHolder).sc.writeFile(path, content)
	})
}

// PluginSFTPUploadB64 通过 SFTP 上传本地文件（二进制安全，b64 为文件内容的 base64）
func PluginSFTPUploadB64(conn model.PluginConn, remoteDir, name, b64 string) error {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return err
	}
	remotePath := joinRemotePath(remoteDir, name)
	return withConn(connKey(conn), sftpFactory(conn), func(v interface{}) error {
		return v.(*sftpHolder).sc.writeFileBytes(remotePath, data)
	})
}

// PluginSFTPRename 重命名 / 移动远端文件或目录
func PluginSFTPRename(conn model.PluginConn, oldPath, newPath string) error {
	if strings.TrimSpace(oldPath) == "" || strings.TrimSpace(newPath) == "" {
		return fmt.Errorf("原路径与新路径不能为空")
	}
	return withConn(connKey(conn), sftpFactory(conn), func(v interface{}) error {
		return v.(*sftpHolder).sc.rename(oldPath, newPath)
	})
}

// PluginSFTPDownload 下载远端文件到本地（弹出保存对话框），返回本地保存路径
func PluginSFTPDownload(b bus.Bus, conn model.PluginConn, remotePath, name string) (string, error) {
	var data []byte
	err := withConn(connKey(conn), sftpFactory(conn), func(v interface{}) error {
		var e error
		data, e = v.(*sftpHolder).sc.readFileBytes(remotePath)
		return e
	})
	if err != nil {
		return "", err
	}
	return saveDownloadedFile(b, name, remotePath, data)
}

// saveDownloadedFile 弹出保存对话框并将字节写入本地文件，返回本地路径（用户取消返回空串）
func saveDownloadedFile(b bus.Bus, name, remotePath string, data []byte) (string, error) {
	if name == "" {
		name = pathBaseName(remotePath)
	}
	local, err := b.SaveFileDialog(runtime.SaveDialogOptions{
		Title:           "保存文件",
		DefaultFilename: name,
	})
	if err != nil {
		return "", err
	}
	if local == "" {
		return "", nil
	}
	if err := os.WriteFile(local, data, 0o644); err != nil {
		return "", err
	}
	return local, nil
}

// pathBaseName 取远端路径的文件名部分
func pathBaseName(p string) string {
	p = strings.TrimRight(p, "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	if p == "" {
		return "download"
	}
	return p
}

// PluginSFTPMkdir 创建远端目录
func PluginSFTPMkdir(conn model.PluginConn, path string) error {
	return withConn(connKey(conn), sftpFactory(conn), func(v interface{}) error {
		return v.(*sftpHolder).sc.mkdir(path)
	})
}

// PluginSFTPDelete 删除远端文件或目录
func PluginSFTPDelete(conn model.PluginConn, path string) error {
	return withConn(connKey(conn), sftpFactory(conn), func(v interface{}) error {
		h := v.(*sftpHolder)
		isDir, err := h.sc.stat(path)
		if err != nil {
			return err
		}
		return h.sc.remove(path, isDir)
	})
}

// ===================== FTP =====================

type ftpConn struct {
	c *textproto.Conn
}

func ftpDial(conn model.PluginConn) (*ftpConn, error) {
	addr := net.JoinHostPort(conn.Host, strconv.Itoa(portOrDefault(conn.Port, 21)))
	tc, err := net.DialTimeout("tcp", addr, 8*time.Second)
	if err != nil {
		return nil, err
	}
	// 清除建连阶段的超时，使连接可被连接池长期复用
	tc.SetDeadline(time.Time{})
	c := textproto.NewConn(tc)
	if _, _, err := c.ReadResponse(220); err != nil {
		c.Close()
		return nil, err
	}
	if err := c.PrintfLine("USER %s", conn.Username); err != nil {
		c.Close()
		return nil, err
	}
	code, _, err := c.ReadResponse(331)
	if err != nil && code != 230 {
		c.Close()
		return nil, err
	}
	if code == 331 {
		if err := c.PrintfLine("PASS %s", conn.Password); err != nil {
			c.Close()
			return nil, err
		}
		if _, _, err := c.ReadResponse(230); err != nil {
			c.Close()
			return nil, err
		}
	}
	c.PrintfLine("TYPE I")
	c.ReadResponse(200)
	return &ftpConn{c: c}, nil
}

func (f *ftpConn) Close() error {
	f.c.PrintfLine("QUIT")
	return f.c.Close()
}

func (f *ftpConn) pasv() (net.Conn, error) {
	if err := f.c.PrintfLine("PASV"); err != nil {
		return nil, err
	}
	_, line, err := f.c.ReadResponse(227)
	if err != nil {
		return nil, err
	}
	// 形如 227 Entering Passive Mode (h1,h2,h3,h4,p1,p2)
	start := strings.Index(line, "(")
	end := strings.Index(line, ")")
	if start < 0 || end < 0 {
		return nil, fmt.Errorf("无法解析 PASV 响应：%s", line)
	}
	parts := strings.Split(line[start+1:end], ",")
	if len(parts) != 6 {
		return nil, fmt.Errorf("无法解析 PASV 地址：%s", line)
	}
	ip := strings.Join(parts[:4], ".")
	p1, _ := strconv.Atoi(strings.TrimSpace(parts[4]))
	p2, _ := strconv.Atoi(strings.TrimSpace(parts[5]))
	port := p1*256 + p2
	return net.DialTimeout("tcp", net.JoinHostPort(ip, strconv.Itoa(port)), 8*time.Second)
}

func ftpFactory(conn model.PluginConn) func() (interface{}, func(), error) {
	return func() (interface{}, func(), error) {
		fc, err := ftpDial(conn)
		if err != nil {
			return nil, nil, err
		}
		return fc, func() { fc.Close() }, nil
	}
}

// PluginFTPList 列出目录
func PluginFTPList(conn model.PluginConn, path string) ([]FileInfo, error) {
	var out []FileInfo
	err := withConn(connKey(conn), ftpFactory(conn), func(v interface{}) error {
		fc := v.(*ftpConn)
		dc, e := fc.pasv()
		if e != nil {
			return e
		}
		defer dc.Close()
		cmd := "LIST"
		if path != "" {
			cmd += " " + path
		}
		fc.c.PrintfLine(cmd)
		fc.c.ReadResponse(150)
		data, _ := io.ReadAll(dc)
		fc.c.ReadResponse(226)
		out = parseFTPList(string(data), path)
		return nil
	})
	return out, err
}

func parseFTPList(s, parent string) []FileInfo {
	lines := strings.Split(s, "\n")
	out := []FileInfo{}
	for _, ln := range lines {
		ln = strings.TrimRight(ln, "\r")
		if strings.TrimSpace(ln) == "" {
			continue
		}
		fields := strings.Fields(ln)
		if len(fields) < 9 {
			continue
		}
		isDir := strings.HasPrefix(fields[0], "d")
		name := strings.Join(fields[8:], " ")
		var size int64
		if !isDir {
			size, _ = strconv.ParseInt(fields[4], 10, 64)
		}
		p := parent
		if p == "" || p == "/" {
			p = "/" + name
		} else {
			p = strings.TrimRight(p, "/") + "/" + name
		}
		out = append(out, FileInfo{Name: name, Path: p, IsDir: isDir, Size: size, Mode: fields[0]})
	}
	return out
}

// PluginFTPRead 读取文件（仅文本，最大 5MB）
func PluginFTPRead(conn model.PluginConn, path string) (string, error) {
	var out string
	err := withConn(connKey(conn), ftpFactory(conn), func(v interface{}) error {
		fc := v.(*ftpConn)
		dc, e := fc.pasv()
		if e != nil {
			return e
		}
		defer dc.Close()
		fc.c.PrintfLine("RETR %s", path)
		fc.c.ReadResponse(150)
		data, _ := io.ReadAll(io.LimitReader(dc, 5*1024*1024))
		fc.c.ReadResponse(226)
		out = string(data)
		return nil
	})
	return out, err
}

// PluginFTPWrite 上传文件
func PluginFTPWrite(conn model.PluginConn, path, content string) error {
	return withConn(connKey(conn), ftpFactory(conn), func(v interface{}) error {
		fc := v.(*ftpConn)
		dc, e := fc.pasv()
		if e != nil {
			return e
		}
		defer dc.Close()
		fc.c.PrintfLine("STOR %s", path)
		fc.c.ReadResponse(150)
		dc.Write([]byte(content))
		_, _, e = fc.c.ReadResponse(226)
		return e
	})
}

// PluginFTPUploadB64 通过 FTP 上传本地文件（二进制安全，b64 为文件内容的 base64）
func PluginFTPUploadB64(conn model.PluginConn, remoteDir, name, b64 string) error {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return err
	}
	remotePath := joinRemotePath(remoteDir, name)
	return withConn(connKey(conn), ftpFactory(conn), func(v interface{}) error {
		fc := v.(*ftpConn)
		dc, e := fc.pasv()
		if e != nil {
			return e
		}
		fc.c.PrintfLine("STOR %s", remotePath)
		if _, _, e = fc.c.ReadResponse(150); e != nil {
			dc.Close()
			return e
		}
		_, e = dc.Write(data)
		dc.Close()
		if e != nil {
			return e
		}
		_, _, e = fc.c.ReadResponse(226)
		return e
	})
}

// PluginFTPDownload 下载远端文件到本地（弹出保存对话框），返回本地保存路径
func PluginFTPDownload(b bus.Bus, conn model.PluginConn, remotePath, name string) (string, error) {
	var data []byte
	err := withConn(connKey(conn), ftpFactory(conn), func(v interface{}) error {
		fc := v.(*ftpConn)
		dc, e := fc.pasv()
		if e != nil {
			return e
		}
		defer dc.Close()
		fc.c.PrintfLine("RETR %s", remotePath)
		if _, _, e = fc.c.ReadResponse(150); e != nil {
			return e
		}
		b, e := io.ReadAll(io.LimitReader(dc, 200*1024*1024))
		if e != nil {
			return e
		}
		data = b
		fc.c.ReadResponse(226)
		return nil
	})
	if err != nil {
		return "", err
	}
	return saveDownloadedFile(b, name, remotePath, data)
}

// PluginFTPRename 重命名 / 移动远端文件或目录（RNFR + RNTO）
func PluginFTPRename(conn model.PluginConn, oldPath, newPath string) error {
	if strings.TrimSpace(oldPath) == "" || strings.TrimSpace(newPath) == "" {
		return fmt.Errorf("原路径与新路径不能为空")
	}
	return withConn(connKey(conn), ftpFactory(conn), func(v interface{}) error {
		fc := v.(*ftpConn)
		if e := fc.c.PrintfLine("RNFR %s", strings.TrimRight(oldPath, "/")); e != nil {
			return e
		}
		if _, _, e := fc.c.ReadResponse(350); e != nil {
			return e
		}
		if e := fc.c.PrintfLine("RNTO %s", strings.TrimRight(newPath, "/")); e != nil {
			return e
		}
		_, _, e := fc.c.ReadResponse(250)
		return e
	})
}

// PluginFTPMkdir 创建目录
func PluginFTPMkdir(conn model.PluginConn, path string) error {
	return withConn(connKey(conn), ftpFactory(conn), func(v interface{}) error {
		fc := v.(*ftpConn)
		fc.c.PrintfLine("MKD %s", path)
		_, _, e := fc.c.ReadResponse(257)
		return e
	})
}

// PluginFTPDelete 删除文件或目录
func PluginFTPDelete(conn model.PluginConn, path string) error {
	return withConn(connKey(conn), ftpFactory(conn), func(v interface{}) error {
		fc := v.(*ftpConn)
		if strings.HasSuffix(path, "/") {
			fc.c.PrintfLine("RMD %s", strings.TrimRight(path, "/"))
		} else {
			fc.c.PrintfLine("DELE %s", path)
		}
		_, _, e := fc.c.ReadResponse(250)
		return e
	})
}

// ===================== 剪贴板操作（供前端直接调用） =====================

// PluginSetClipboard 写入系统剪贴板（选中历史项后回填）
func PluginSetClipboard(b bus.Bus, text string) error {
	return b.ClipboardSetText(text)
}
