package plugins

import (
	"fmt"
	"io"
	"strings"

	"apitool/internal/model"
	"golang.org/x/crypto/ssh"
)

// sftpClient 基于 SSH 的 SFTP 客户端（使用 no client 库，直接走 SSH 子系统的二进制协议，
// 仅实现本项目需要的最小子集：列目录 / 读 / 写 / 重命名 / 删除 / 建目录）。
type sftpClient struct {
	c          *ssh.Client
	chanIn     io.WriteCloser
	chanOut    io.Reader
	nextID     uint32
	openHandles map[string]bool // 仅占位，本实现单句柄顺序操作
}

func openSFTP(conn model.PluginConn) (*ssh.Client, *sftpClient, error) {
	client, err := sshDial(conn)
	if err != nil {
		return nil, nil, err
	}
	sc, err := newSFTPClient(client)
	if err != nil {
		client.Close()
		return nil, nil, err
	}
	return client, sc, nil
}

func newSFTPClient(c *ssh.Client) (*sftpClient, error) {
	ch, _, err := c.OpenChannel("subsystem", []byte("sftp"))
	if err != nil {
		return nil, err
	}
	// 发送 SSH_FXP_INIT（版本 3）并等待对端版本包
	if _, err := ch.SendRequest("initialize", true, nil); err != nil {
		ch.Close()
		return nil, err
	}
	buf := make([]byte, 4)
	if _, e := io.ReadFull(ch, buf); e != nil {
		ch.Close()
		return nil, e
	}
	return &sftpClient{c: c, chanIn: ch, chanOut: ch, nextID: 1}, nil
}

func (s *sftpClient) Close() error {
	if s.chanIn != nil {
		return s.chanIn.Close()
	}
	return nil
}

// ---- 协议常量 ----
const (
	sshFxpInit          = 1
	sshFxpVersion       = 2
	sshFxpOpen          = 3
	sshFxpClose         = 4
	sshFxpRead          = 5
	sshFxpWrite         = 6
	sshFxpStat          = 17
	sshFxpFstat         = 8
	sshFxpOpendir       = 11
	sshFxpReaddir       = 12
	sshFxpRemove        = 13
	sshFxpMkdir         = 14
	sshFxpRmdir         = 15
	sshFxpRealpath      = 16
	sshFxpRename        = 18
	sshFxpStatus        = 101
	sshFxpHandle        = 102
	sshFxpData          = 103
	sshFxpName          = 104
	sshFxpAttrs         = 105

	sshFxfRead  = 0x00000001
	sshFxfWrite = 0x00000002
	sshFxfCreat = 0x00000008
	sshFxfTrunc = 0x00000010
)

func (s *sftpClient) nextPacketID() uint32 {
	id := s.nextID
	s.nextID++
	return id
}

func putUint32(b []byte, v uint32) { b[0], b[1], b[2], b[3] = byte(v>>24), byte(v>>16), byte(v>>8), byte(v) }
func getUint32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}
func putUint64(b []byte, v uint64) {
	putUint32(b[:4], uint32(v>>32))
	putUint32(b[4:], uint32(v))
}

func (s *sftpClient) send(payload []byte) error {
	hdr := make([]byte, 4)
	putUint32(hdr, uint32(len(payload)))
	if _, e := s.chanIn.Write(hdr); e != nil {
		return e
	}
	_, e := s.chanIn.Write(payload)
	return e
}

func (s *sftpClient) recv() ([]byte, error) {
	hdr := make([]byte, 4)
	if _, e := io.ReadFull(s.chanOut, hdr); e != nil {
		return nil, e
	}
	n := getUint32(hdr)
	buf := make([]byte, n)
	if _, e := io.ReadFull(s.chanOut, buf); e != nil {
		return nil, e
	}
	return buf, nil
}

// readResponse 读取一个响应包，返回 (type, id, payload)。
func (s *sftpClient) readResponse() (uint8, uint32, []byte, error) {
	buf, err := s.recv()
	if err != nil {
		return 0, 0, nil, err
	}
	if len(buf) < 5 {
		return 0, 0, nil, fmt.Errorf("响应包过短")
	}
	typ := buf[0]
	id := getUint32(buf[1:5])
	return typ, id, buf[5:], nil
}

func (s *sftpClient) waitStatus(id uint32) error {
	for {
		typ, rid, payload, err := s.readResponse()
		if err != nil {
			return err
		}
		if rid != id {
			continue
		}
		if typ == sshFxpStatus {
			if len(payload) < 4 {
				return fmt.Errorf("状态包异常")
			}
			code := getUint32(payload)
			if code == 0 {
				return nil
			}
			msg := ""
			if len(payload) > 4 {
				msg = string(payload[4:])
			}
			return fmt.Errorf("SFTP 错误 %d: %s", code, msg)
		}
		// 非状态包（如 data/handle/name）应已在对端队列中按 id 对应；这里忽略不匹配
	}
}

func (s *sftpClient) sendRequest(typ uint8, payload []byte) (uint32, error) {
	id := s.nextPacketID()
	p := append([]byte{typ}, 0, 0, 0, 0)
	putUint32(p[1:5], id)
	p = append(p, payload...)
	if err := s.send(p); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *sftpClient) openDir(p string) (string, error) {
	id, err := s.sendRequest(sshFxpOpendir, appendString(p))
	if err != nil {
		return "", err
	}
	for {
		typ, rid, payload, err := s.readResponse()
		if err != nil {
			return "", err
		}
		if rid != id {
			continue
		}
		switch typ {
		case sshFxpHandle:
			handle := payload
			return string(handle), nil
		case sshFxpStatus:
			return "", fmt.Errorf("打开目录失败: %s", string(payload[4:]))
		}
	}
}

func (s *sftpClient) listDir(dir string) ([]FileInfo, error) {
	handle, err := s.openDir(dir)
	if err != nil {
		return nil, err
	}
	defer s.closeHandle(handle)
	out := []FileInfo{}
	for {
		id, err := s.sendRequest(sshFxpReaddir, appendString(handle))
		if err != nil {
			return nil, err
		}
		for {
			typ, rid, payload, err := s.readResponse()
			if err != nil {
				return nil, err
			}
			if rid != id {
				continue
			}
			if typ == sshFxpStatus {
				return out, nil // EOF
			}
			if typ != sshFxpName {
				return out, nil
			}
			infos := parseNamePayload(payload)
			out = append(out, infos...)
			break
		}
	}
}

func parseNamePayload(payload []byte) []FileInfo {
	// payload: count(4) { filename len(4)+bytes, longname len(4)+bytes, attrs len(4)+bytes }
	if len(payload) < 4 {
		return nil
	}
	count := getUint32(payload)
	pos := 4
	infos := []FileInfo{}
	for i := uint32(0); i < count; i++ {
		if pos+4 > len(payload) {
			break
		}
		nlen := int(getUint32(payload[pos:]))
		pos += 4
		if pos+nlen > len(payload) {
			break
		}
		name := string(payload[pos : pos+nlen])
		pos += nlen
		// longname
		if pos+4 > len(payload) {
			break
		}
		llen := int(getUint32(payload[pos:]))
		pos += 4
		if pos+llen > len(payload) {
			break
		}
		longname := string(payload[pos : pos+llen])
		pos += llen
		// attrs（len + 内容，跳过）
		if pos+4 > len(payload) {
			break
		}
		attrsLen := int(getUint32(payload[pos:]))
		pos += 4 + attrsLen

		if name == "." || name == ".." {
			continue
		}
		fi := FileInfo{Name: name, Path: name, IsDir: strings.HasPrefix(longname, "d")}
		// 从 longname 解析大小与权限（ls -l 风格）
		fields := strings.Fields(longname)
		if len(fields) >= 2 {
			fi.Mode = fields[0]
		}
		if !fi.IsDir && len(fields) >= 5 {
			var sz int64
			fmt.Sscanf(fields[4], "%d", &sz)
			fi.Size = sz
		}
		infos = append(infos, fi)
	}
	return infos
}

func (s *sftpClient) closeHandle(handle string) error {
	id, err := s.sendRequest(sshFxpClose, appendString(handle))
	if err != nil {
		return err
	}
	return s.waitStatus(id)
}

func (s *sftpClient) readFile(p string) (string, error) {
	data, err := s.readFileBytes(p)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *sftpClient) readFileBytes(p string) ([]byte, error) {
	handle, err := s.openFile(p, sshFxfRead)
	if err != nil {
		return nil, err
	}
	defer s.closeHandle(handle)
	var out []byte
	const chunk = 32 * 1024
	offset := uint64(0)
	for {
		// 构造 READ 请求：handle(len+str) + offset(8) + len(4)
		pl := appendString(handle)
		pl = append(pl, 0, 0, 0, 0, 0, 0, 0, 0)
		putUint64(pl[len(pl)-8:], offset)
		pl = append(pl, 0, 0, 0, 0)
		putUint32(pl[len(pl)-4:], chunk)
		id, err := s.sendRequest(sshFxpRead, pl)
		if err != nil {
			return nil, err
		}
		for {
			typ, rid, payload, err := s.readResponse()
			if err != nil {
				return nil, err
			}
			if rid != id {
				continue
			}
			if typ == sshFxpStatus {
				return out, nil // EOF
			}
			if typ != sshFxpData {
				return out, nil
			}
			if len(payload) < 4 {
				return out, nil
			}
			dlen := int(getUint32(payload))
			if len(payload) < 4+dlen {
				return out, fmt.Errorf("数据长度异常")
			}
			out = append(out, payload[4:4+dlen]...)
			if dlen < chunk {
				return out, nil
			}
			offset += uint64(dlen)
			break
		}
	}
}

func (s *sftpClient) openFile(p string, flags uint32) (string, error) {
	// OPEN: filename(len+str) + pflags(4) + attrs(4, 全 0)
	pl := appendString(p)
	pl = append(pl, 0, 0, 0, 0)
	putUint32(pl[len(pl)-4:], flags)
	pl = append(pl, 0, 0, 0, 0) // attrs 长度 0
	id, err := s.sendRequest(sshFxpOpen, pl)
	if err != nil {
		return "", err
	}
	for {
		typ, rid, payload, err := s.readResponse()
		if err != nil {
			return "", err
		}
		if rid != id {
			continue
		}
		if typ == sshFxpHandle {
			return string(payload), nil
		}
		if typ == sshFxpStatus {
			return "", fmt.Errorf("打开文件失败: %s", string(payload[4:]))
		}
	}
}

func (s *sftpClient) writeFile(p, content string) error {
	return s.writeFileBytes(p, []byte(content))
}

func (s *sftpClient) writeFileBytes(p string, data []byte) error {
	flags := uint32(sshFxfWrite | sshFxfCreat | sshFxfTrunc)
	handle, err := s.openFile(p, flags)
	if err != nil {
		return err
	}
	defer s.closeHandle(handle)
	const chunk = 32 * 1024
	offset := uint64(0)
	for len(data) > 0 {
		n := chunk
		if n > len(data) {
			n = len(data)
		}
		pl := appendString(handle)
		pl = append(pl, 0, 0, 0, 0, 0, 0, 0, 0)
		putUint64(pl[len(pl)-8:], offset)
		pl = append(pl, 0, 0, 0, 0)
		putUint32(pl[len(pl)-4:], uint32(n))
		pl = append(pl, data[:n]...)
		id, err := s.sendRequest(sshFxpWrite, pl)
		if err != nil {
			return err
		}
		if err := s.waitStatus(id); err != nil {
			return err
		}
		data = data[n:]
		offset += uint64(n)
	}
	return nil
}

func (s *sftpClient) rename(oldPath, newPath string) error {
	// RENAME: oldpath(len+str) + newpath(len+str)
	pl := appendString(oldPath)
	pl = append(pl, appendString(newPath)...)
	id, err := s.sendRequest(sshFxpRename, pl)
	if err != nil {
		return err
	}
	return s.waitStatus(id)
}

func (s *sftpClient) stat(p string) (bool, error) {
	pl := appendString(p)
	id, err := s.sendRequest(sshFxpStat, pl)
	if err != nil {
		return false, err
	}
	for {
		typ, rid, payload, err := s.readResponse()
		if err != nil {
			return false, err
		}
		if rid != id {
			continue
		}
		if typ == sshFxpAttrs {
			// 解析权限位（payload: flags(4) + 至少 mode(4)）
			if len(payload) >= 8 {
				mode := getUint32(payload[4:8])
				return mode&0o040000 != 0, nil
			}
			return false, nil
		}
		if typ == sshFxpStatus {
			return false, fmt.Errorf("stat 失败: %s", string(payload[4:]))
		}
	}
}

func (s *sftpClient) remove(p string, isDir bool) error {
	if isDir {
		return s.rmdir(p)
	}
	pl := appendString(p)
	id, err := s.sendRequest(sshFxpRemove, pl)
	if err != nil {
		return err
	}
	return s.waitStatus(id)
}

func (s *sftpClient) rmdir(p string) error {
	pl := appendString(p)
	id, err := s.sendRequest(sshFxpRmdir, pl)
	if err != nil {
		return err
	}
	return s.waitStatus(id)
}

func (s *sftpClient) mkdir(p string) error {
	// MKDIR: path(len+str) + attrs(4, 0)
	pl := appendString(p)
	pl = append(pl, 0, 0, 0, 0)
	id, err := s.sendRequest(sshFxpMkdir, pl)
	if err != nil {
		return err
	}
	return s.waitStatus(id)
}

// appendString 将字符串编码为 SFTP 长度前缀 + 字节（用于协议字段）。
func appendString(s string) []byte {
	b := make([]byte, 4+len(s))
	putUint32(b, uint32(len(s)))
	copy(b[4:], s)
	return b
}
