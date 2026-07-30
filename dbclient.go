package main

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// ===================== MySQL 最小客户端 =====================

const (
	cliProtocol41    = 0x200
	cliPluginAuth    = 0x80000
	cliSecureConn    = 0x8000
	cliConnectWithDB = 0x8
)

type mysqlConn struct {
	conn     net.Conn
	r        *bufio.Reader
	seq      byte
	scramble []byte
}

func openMySQL(conn PluginConn) (*mysqlConn, error) {
	addr := net.JoinHostPort(conn.Host, strconv.Itoa(portOrDefault(conn.Port, 3306)))
	c, err := net.DialTimeout("tcp", addr, 6*time.Second)
	if err != nil {
		return nil, err
	}
	c.SetDeadline(time.Now().Add(15 * time.Second))
	mc := &mysqlConn{conn: c, r: bufio.NewReader(c)}
	if err := mc.handshake(conn.Username, conn.Password, conn.Database); err != nil {
		c.Close()
		return nil, err
	}
	return mc, nil
}

func (mc *mysqlConn) Close() error { return mc.conn.Close() }

func readPacket(r *bufio.Reader) ([]byte, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}
	length := int(header[0]) | int(header[1])<<8 | int(header[2])<<16
	if length == 0 {
		return []byte{}, nil
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func (mc *mysqlConn) writePacket(payload []byte) error {
	length := len(payload)
	header := []byte{byte(length), byte(length >> 8), byte(length >> 16), mc.seq}
	mc.seq++
	if _, err := mc.conn.Write(header); err != nil {
		return err
	}
	_, err := mc.conn.Write(payload)
	return err
}

func parseMySQLError(p []byte) error {
	// 0xff, 2-byte error code, sql state marker '#', 5-byte sql state, message
	if len(p) < 9 {
		return fmt.Errorf("MySQL 错误")
	}
	return fmt.Errorf("MySQL 错误：%s", string(p[9:]))
}

func mysqlNativePassword(password string, scramble []byte) []byte {
	if password == "" || len(scramble) < 20 {
		return nil
	}
	hash1 := sha1.Sum([]byte(password))
	hash2 := sha1.Sum(hash1[:])
	hash3 := sha1.Sum(append(append([]byte{}, scramble[:20]...), hash2[:]...))
	out := make([]byte, 20)
	for i := 0; i < 20; i++ {
		out[i] = hash1[i] ^ hash3[i]
	}
	return out
}

func (mc *mysqlConn) handshake(user, pass, db string) error {
	p, err := readPacket(mc.r)
	if err != nil {
		return err
	}
	if len(p) == 0 || p[0] != 10 {
		return fmt.Errorf("不支持的 MySQL 协议版本")
	}
	// server version (null terminated)
	i := 1
	for p[i] != 0 {
		i++
	}
	i++    // skip null
	i += 4 // thread id
	scramble1 := p[i : i+8]
	i += 8
	i++    // filler
	i += 2 // caps low
	i++    // charset
	i += 2 // status
	i += 2 // caps high
	authLen := int(p[i])
	i++
	i += 10 // reserved
	// scramble2 until null
	end := i
	for p[end] != 0 {
		end++
	}
	scramble2 := p[i:end]
	mc.scramble = append(append([]byte{}, scramble1...), scramble2...)
	if len(mc.scramble) > 20 {
		mc.scramble = mc.scramble[:20]
	}
	// auth plugin name (optional, may be present after scramble null)
	_ = authLen

	// build handshake response
	caps := uint32(cliProtocol41 | cliPluginAuth | cliSecureConn)
	if db != "" {
		caps |= cliConnectWithDB
	}
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, caps)
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0)) // max packet
	buf.WriteByte(33)                                      // utf8mb4
	buf.Write(make([]byte, 23))                            // reserved
	buf.WriteString(user)
	buf.WriteByte(0)
	np := mysqlNativePassword(pass, mc.scramble)
	buf.WriteByte(byte(len(np)))
	buf.Write(np)
	if db != "" {
		buf.WriteString(db)
	}
	buf.WriteByte(0)
	buf.WriteString("mysql_native_password")
	buf.WriteByte(0)

	mc.seq = 1
	if err := mc.writePacket(buf.Bytes()); err != nil {
		return err
	}
	return mc.readAuthLoop(pass)
}

func (mc *mysqlConn) readAuthLoop(pass string) error {
	for {
		p, err := readPacket(mc.r)
		if err != nil {
			return err
		}
		if len(p) == 0 {
			continue
		}
		switch p[0] {
		case 0x00:
			return nil
		case 0xff:
			return parseMySQLError(p)
		case 0xfe:
			// AuthSwitchRequest (plugin name + scramble)
			return mc.handleAuthSwitch(p, pass)
		case 0x01:
			// AuthMoreData (caching_sha2_password)
			return mc.handleAuthMoreData(p, pass)
		}
	}
}

func (mc *mysqlConn) handleAuthSwitch(p []byte, pass string) error {
	i := 1
	end := bytes.IndexByte(p[i:], 0)
	if end < 0 {
		return fmt.Errorf("认证切换数据异常")
	}
	plugin := string(p[i : i+end])
	i += end + 1
	end2 := bytes.IndexByte(p[i:], 0)
	newScramble := p[i : i+end2]
	var resp []byte
	switch plugin {
	case "mysql_native_password":
		resp = mysqlNativePassword(pass, newScramble)
	case "caching_sha2_password":
		pw := []byte(pass)
		resp = append([]byte{byte(len(pw))}, pw...)
	default:
		return fmt.Errorf("不支持的认证插件：%s", plugin)
	}
	mc.seq++
	if err := mc.writePacket(resp); err != nil {
		return err
	}
	return mc.readAuthLoop(pass)
}

func (mc *mysqlConn) handleAuthMoreData(p []byte, pass string) error {
	if len(p) > 1 && p[1] == 0x03 {
		// fast auth complete, next should be OK
		return mc.readAuthLoop(pass)
	}
	// full auth: request public key
	mc.seq++
	if err := mc.writePacket([]byte{0x02}); err != nil {
		return err
	}
	kp, err := readPacket(mc.r)
	if err != nil {
		return err
	}
	if len(kp) == 0 || kp[0] != 0x01 {
		return fmt.Errorf("获取 RSA 公钥失败")
	}
	pemBytes := kp[1:]
	enc, err := rsaEncryptPassword(pemBytes, pass)
	if err != nil {
		return err
	}
	// payload: 4-byte length prefix + encrypted
	payload := append([]byte{}, byte(len(enc)), byte(len(enc)>>8), byte(len(enc)>>16), byte(len(enc)>>24))
	payload = append(payload, enc...)
	mc.seq++
	if err := mc.writePacket(payload); err != nil {
		return err
	}
	return mc.readAuthLoop(pass)
}

func rsaEncryptPassword(pemBytes []byte, password string) ([]byte, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("无法解析 RSA 公钥")
	}
	pubIface, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	pub, ok := pubIface.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("不是 RSA 公钥")
	}
	h := sha256.Sum256([]byte(password))
	return rsa.EncryptPKCS1v15(rand.Reader, pub, h[:])
}

func (mc *mysqlConn) query(sql string) (*DBRow, error) {
	mc.seq = 0
	payload := append([]byte{0x03}, []byte(sql)...)
	if err := mc.writePacket(payload); err != nil {
		return nil, err
	}
	p, err := readPacket(mc.r)
	if err != nil {
		return nil, err
	}
	if len(p) == 0 {
		return &DBRow{}, nil
	}
	switch p[0] {
	case 0xff:
		return nil, parseMySQLError(p)
	case 0x00, 0xfe:
		return &DBRow{}, nil
	}
	n, _ := readLenEncInt(p, 0)
	cols := make([]string, n)
	for i := uint64(0); i < n; i++ {
		cp, err := readPacket(mc.r)
		if err != nil {
			return nil, err
		}
		cols[i] = columnName(cp)
	}
	readPacket(mc.r) // EOF after column defs
	rows := [][]string{}
	for {
		rp, err := readPacket(mc.r)
		if err != nil {
			break
		}
		if len(rp) > 0 && rp[0] == 0xfe && len(rp) <= 5 {
			break
		}
		row := parseRow(rp, int(n))
		rows = append(rows, row)
	}
	return &DBRow{Columns: cols, Rows: rows}, nil
}

func readLenEncInt(b []byte, off int) (uint64, int) {
	if off >= len(b) {
		return 0, off
	}
	switch b[off] {
	case 0xfb:
		return 0, off + 1
	case 0xfc:
		return uint64(b[off+1]) | uint64(b[off+2])<<8, off + 3
	case 0xfd:
		return uint64(b[off+1]) | uint64(b[off+2])<<8 | uint64(b[off+3])<<16, off + 4
	case 0xfe:
		return uint64(b[off+1]) | uint64(b[off+2])<<8 | uint64(b[off+3])<<16 |
			uint64(b[off+4])<<24 | uint64(b[off+5])<<32 | uint64(b[off+6])<<40 |
			uint64(b[off+7])<<48 | uint64(b[off+8])<<56, off + 9
	default:
		return uint64(b[off]), off + 1
	}
}

func readLenEncStr(b []byte, off int) (string, int) {
	n, noff := readLenEncInt(b, off)
	if noff > len(b) || int(n) > len(b)-noff {
		return "", noff
	}
	return string(b[noff : noff+int(n)]), noff + int(n)
}

func columnName(cp []byte) string {
	// skip catalog, db, table, org_table (4 lenenc strings)
	off := 0
	for k := 0; k < 4; k++ {
		_, off = readLenEncStr(cp, off)
	}
	name, _ := readLenEncStr(cp, off)
	return name
}

func parseRow(rp []byte, n int) []string {
	row := make([]string, n)
	off := 0
	for i := 0; i < n; i++ {
		v, noff := readLenEncStr(rp, off)
		row[i] = v
		off = noff
	}
	return row
}

// ===================== PostgreSQL 最小客户端（MD5 认证） =====================

type pgConn struct {
	conn  net.Conn
	r     *bufio.Reader
	pass  string
	_user string
}

func openPostgres(conn PluginConn) (*pgConn, error) {
	addr := net.JoinHostPort(conn.Host, strconv.Itoa(portOrDefault(conn.Port, 5432)))
	c, err := net.DialTimeout("tcp", addr, 6*time.Second)
	if err != nil {
		return nil, err
	}
	c.SetDeadline(time.Now().Add(15 * time.Second))
	pc := &pgConn{conn: c, r: bufio.NewReader(c), pass: conn.Password, _user: conn.Username}
	if err := pc.startup(conn); err != nil {
		c.Close()
		return nil, err
	}
	return pc, nil
}

func (pc *pgConn) Close() error { return pc.conn.Close() }

func (pc *pgConn) startup(conn PluginConn) error {
	var buf bytes.Buffer
	buf.Write([]byte{0, 0, 0, 0}) // length placeholder
	buf.Write([]byte{0, 3, 0, 0}) // protocol version 3.0
	writeParam(&buf, "user", conn.Username)
	writeParam(&buf, "database", conn.Database)
	writeParam(&buf, "client_encoding", "UTF8")
	writeParam(&buf, "DateStyle", "ISO")
	buf.WriteByte(0) // terminator
	b := buf.Bytes()
	length := int32(len(b))
	binary.BigEndian.PutUint32(b[0:4], uint32(length))
	if _, err := pc.conn.Write(b); err != nil {
		return err
	}
	return pc.authLoop()
}

func writeParam(buf *bytes.Buffer, k, v string) {
	buf.WriteString(k)
	buf.WriteByte(0)
	buf.WriteString(v)
	buf.WriteByte(0)
}

func (pc *pgConn) readMsg() (byte, []byte, error) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(pc.r, header); err != nil {
		return 0, nil, err
	}
	typ := header[0]
	length := int(binary.BigEndian.Uint32(header[1:5])) - 4
	if length < 0 {
		length = 0
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(pc.r, body); err != nil {
		return 0, nil, err
	}
	return typ, body, nil
}

func (pc *pgConn) authLoop() error {
	for {
		typ, body, err := pc.readMsg()
		if err != nil {
			return err
		}
		switch typ {
		case 'R': // Authentication
			code := binary.BigEndian.Uint32(body[0:4])
			switch code {
			case 0: // OK
				return nil
			case 5: // MD5
				salt := string(body[4:8])
				resp := pgMD5(pc._user, pc.pass, salt)
				if err := pc.sendPassword(resp); err != nil {
					return err
				}
			default:
				return fmt.Errorf("PostgreSQL 使用了暂不支持的认证方式（%d），请改用 md5 认证", code)
			}
		case 'E':
			return fmt.Errorf("PostgreSQL 错误：%s", pgErrMsg(body))
		case 'S', 'K', 'N':
			// ParameterStatus / BackendKeyData / Notice -> ignore
		case 'Z': // ReadyForQuery
			return nil
		}
	}
}

func pgErrMsg(body []byte) string {
	parts := strings.Split(string(body), "\x00")
	if len(parts) > 0 {
		return strings.ReplaceAll(parts[0], "\x00", " ")
	}
	return "未知错误"
}

func md5hex(s string) string {
	sum := md5.Sum([]byte(s))
	return fmt.Sprintf("%x", sum)
}

func pgMD5(user, password, salt string) string {
	h1 := md5hex(md5hex(user+":"+password) + salt)
	return "md5" + h1
}

func (pc *pgConn) sendPassword(pw string) error {
	body := append([]byte(pw), 0)
	msg := make([]byte, 5+len(body))
	msg[0] = 'p'
	binary.BigEndian.PutUint32(msg[1:5], uint32(4+len(body)))
	copy(msg[5:], body)
	_, err := pc.conn.Write(msg)
	return err
}

func (pc *pgConn) query(sql string) (*DBRow, error) {
	body := append([]byte(sql), 0)
	msg := make([]byte, 5+len(body))
	msg[0] = 'Q'
	binary.BigEndian.PutUint32(msg[1:5], uint32(4+len(body)))
	copy(msg[5:], body)
	if _, err := pc.conn.Write(msg); err != nil {
		return nil, err
	}
	cols := []string{}
	rows := [][]string{}
	for {
		typ, body, err := pc.readMsg()
		if err != nil {
			return nil, err
		}
		switch typ {
		case 'T': // RowDescription
			n := int(binary.BigEndian.Uint16(body[0:2]))
			off := 2
			for i := 0; i < n; i++ {
				end := bytes.IndexByte(body[off:], 0)
				cols = append(cols, string(body[off:off+end]))
				off += end + 1 + 18 // skip fixed fields
			}
		case 'D': // DataRow
			n := int(binary.BigEndian.Uint16(body[0:2]))
			off := 2
			row := make([]string, n)
			for i := 0; i < n; i++ {
				l := int(int32(binary.BigEndian.Uint32(body[off : off+4])))
				off += 4
				if l == -1 {
					row[i] = ""
				} else {
					row[i] = string(body[off : off+l])
					off += l
				}
			}
			rows = append(rows, row)
		case 'C', 'I':
			// CommandComplete / Insert
		case 'E':
			return nil, fmt.Errorf("查询错误：%s", pgErrMsg(body))
		case 'Z':
			return &DBRow{Columns: cols, Rows: rows}, nil
		}
	}
}

// ===================== 对外数据库接口 =====================

type dbSession struct {
	dbType string
	mysql  *mysqlConn
	pg     *pgConn
}

func dbFactory(conn PluginConn) func() (interface{}, func(), error) {
	return func() (interface{}, func(), error) {
		switch conn.DbType {
		case "postgres":
			pc, err := openPostgres(conn)
			if err != nil {
				return nil, nil, err
			}
			keepAlive(pc.conn)
			return &dbSession{dbType: "postgres", pg: pc}, func() { pc.Close() }, nil
		default:
			mc, err := openMySQL(conn)
			if err != nil {
				return nil, nil, err
			}
			keepAlive(mc.conn)
			return &dbSession{dbType: "mysql", mysql: mc}, func() { mc.Close() }, nil
		}
	}
}

// PluginDBConnect 连接数据库并列出库
func (a *App) PluginDBConnect(conn PluginConn) (DBInfo, error) {
	var info DBInfo
	err := withConn(connKey(conn), dbFactory(conn), func(v interface{}) error {
		s := v.(*dbSession)
		var rows [][]string
		var e error
		if s.dbType == "postgres" {
			var res *DBRow
			res, e = s.pg.query("SELECT datname FROM pg_database WHERE datistemplate=false ORDER BY datname")
			rows = res.Rows
		} else {
			var res *DBRow
			res, e = s.mysql.query("SHOW DATABASES")
			rows = res.Rows
		}
		if e != nil {
			return e
		}
		dbs := []string{}
		for _, r := range rows {
			if len(r) > 0 {
				dbs = append(dbs, r[0])
			}
		}
		info = DBInfo{Ok: true, Databases: dbs}
		return nil
	})
	if err != nil {
		info = DBInfo{Ok: false, Error: err.Error()}
	}
	return info, nil
}

// PluginDBTables 列出库中的表
func (a *App) PluginDBTables(conn PluginConn, database string) ([]DBTable, error) {
	var tables []DBTable
	err := withConn(connKey(conn), dbFactory(conn), func(v interface{}) error {
		s := v.(*dbSession)
		var rows [][]string
		var e error
		if s.dbType == "postgres" {
			var res *DBRow
			res, e = s.pg.query("SELECT table_name FROM information_schema.tables WHERE table_schema='public' ORDER BY table_name")
			rows = res.Rows
		} else {
			var res *DBRow
			res, e = s.mysql.query("SHOW TABLES FROM `" + database + "`")
			rows = res.Rows
		}
		if e != nil {
			return e
		}
		tables = []DBTable{}
		for _, r := range rows {
			if len(r) > 0 {
				tables = append(tables, DBTable{Name: r[0]})
			}
		}
		return nil
	})
	return tables, err
}

// PluginDBQuery 执行 SQL 查询
func (a *App) PluginDBQuery(conn PluginConn, database, sql string) (DBRow, error) {
	var row DBRow
	err := withConn(connKey(conn), dbFactory(conn), func(v interface{}) error {
		s := v.(*dbSession)
		var res *DBRow
		var e error
		if s.dbType == "postgres" {
			res, e = s.pg.query(sql)
		} else {
			mc := s.mysql
			if database != "" {
				if _, e2 := mc.query("USE `" + database + "`"); e2 != nil {
					return e2
				}
			}
			res, e = mc.query(sql)
		}
		if e != nil {
			return e
		}
		row = *res
		return nil
	})
	if err != nil {
		return DBRow{}, err
	}
	return row, nil
}

// PluginDBDescribe 查看表结构（列定义）
func (a *App) PluginDBDescribe(conn PluginConn, database, table string) (DBRow, error) {
	var row DBRow
	err := withConn(connKey(conn), dbFactory(conn), func(v interface{}) error {
		s := v.(*dbSession)
		var res *DBRow
		var e error
		if s.dbType == "postgres" {
			tbl := strings.ReplaceAll(table, "'", "''")
			res, e = s.pg.query("SELECT column_name, data_type, is_nullable, column_default FROM information_schema.columns WHERE table_schema='public' AND table_name = '" + tbl + "' ORDER BY ordinal_position")
		} else {
			if database != "" {
				if _, e2 := s.mysql.query("USE `" + database + "`"); e2 != nil {
					return e2
				}
			}
			res, e = s.mysql.query("DESCRIBE `" + table + "`")
		}
		if e != nil {
			return e
		}
		row = *res
		return nil
	})
	if err != nil {
		return DBRow{}, err
	}
	return row, nil
}

// PluginDBRowCount 统计表行数
func (a *App) PluginDBRowCount(conn PluginConn, database, table string) (int64, error) {
	var n int64
	err := withConn(connKey(conn), dbFactory(conn), func(v interface{}) error {
		s := v.(*dbSession)
		var res *DBRow
		var e error
		if s.dbType == "postgres" {
			tbl := strings.ReplaceAll(table, "'", "''")
			res, e = s.pg.query("SELECT COUNT(*) FROM \"" + tbl + "\"")
		} else {
			if database != "" {
				if _, e2 := s.mysql.query("USE `" + database + "`"); e2 != nil {
					return e2
				}
			}
			res, e = s.mysql.query("SELECT COUNT(*) FROM `" + table + "`")
		}
		if e != nil {
			return e
		}
		if len(res.Rows) > 0 && len(res.Rows[0]) > 0 {
			n, _ = strconv.ParseInt(res.Rows[0][0], 10, 64)
		}
		return nil
	})
	return n, err
}
