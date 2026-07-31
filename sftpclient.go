package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"time"

	"golang.org/x/crypto/ssh"
)

// 极简 SFTP v3 客户端（基于 golang.org/x/crypto/ssh 子系统，无需额外依赖）
type sftpClient struct {
	w       io.WriteCloser
	r       *bufio.Reader
	session *ssh.Session
	id      uint32
}

func openSFTP(conn PluginConn) (*ssh.Client, *sftpClient, error) {
	client, err := sshDial(conn)
	if err != nil {
		return nil, nil, err
	}
	session, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, nil, err
	}
	if err := session.RequestSubsystem("sftp"); err != nil {
		session.Close()
		client.Close()
		return nil, nil, err
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		client.Close()
		return nil, nil, err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		client.Close()
		return nil, nil, err
	}
	c := &sftpClient{w: stdin, r: bufio.NewReader(stdout), session: session}
	if err := c.init(); err != nil {
		session.Close()
		client.Close()
		return nil, nil, err
	}
	return client, c, nil
}

func (c *sftpClient) Close() error {
	if c.session != nil {
		return c.session.Close()
	}
	return nil
}

func (c *sftpClient) init() error {
	// SSH_FXP_INIT (1) + version 3
	if _, err := c.send(1, []byte{0, 0, 0, 3}); err != nil {
		return err
	}
	typ, _, _, err := c.recv()
	if err != nil {
		return err
	}
	if typ != 2 { // SSH_FXP_VERSION
		return fmt.Errorf("SFTP 初始化失败")
	}
	return nil
}

func (c *sftpClient) send(pktType byte, payload []byte) (uint32, error) {
	c.id++
	id := c.id
	inner := make([]byte, 0, 5+len(payload))
	inner = append(inner, pktType)
	inner = append(inner, byte(id>>24), byte(id>>16), byte(id>>8), byte(id))
	inner = append(inner, payload...)
	buf := make([]byte, 4+len(inner))
	binary.BigEndian.PutUint32(buf[:4], uint32(len(inner)))
	copy(buf[4:], inner)
	_, err := c.w.Write(buf)
	return id, err
}

func (c *sftpClient) recv() (byte, uint32, []byte, error) {
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(c.r, hdr); err != nil {
		return 0, 0, nil, err
	}
	length := binary.BigEndian.Uint32(hdr)
	pkt := make([]byte, length)
	if _, err := io.ReadFull(c.r, pkt); err != nil {
		return 0, 0, nil, err
	}
	typ := pkt[0]
	id := binary.BigEndian.Uint32(pkt[1:5])
	return typ, id, pkt[5:], nil
}

func (c *sftpClient) call(pktType byte, payload []byte) (byte, []byte, error) {
	id, err := c.send(pktType, payload)
	if err != nil {
		return 0, nil, err
	}
	for {
		typ, rid, body, err := c.recv()
		if err != nil {
			return 0, nil, err
		}
		if rid != id {
			continue
		}
		return typ, body, nil
	}
}

func putString(b []byte, s string) []byte {
	var tmp [4]byte
	binary.BigEndian.PutUint32(tmp[:], uint32(len(s)))
	b = append(b, tmp[:]...)
	return append(b, s...)
}

func putUint32(v uint32) []byte {
	return []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
}

func readString(b []byte) (string, []byte) {
	n := binary.BigEndian.Uint32(b[:4])
	return string(b[4 : 4+n]), b[4+n:]
}

func readUint32(b []byte) (uint32, []byte) {
	return binary.BigEndian.Uint32(b[:4]), b[4:]
}

func readUint64(b []byte) (uint64, []byte) {
	return binary.BigEndian.Uint64(b[:8]), b[8:]
}

func packUint64(v uint64) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	return b[:]
}

func expectStatus(typ byte, body []byte) error {
	if typ == 101 { // SSH_FXP_STATUS
		code := binary.BigEndian.Uint32(body[:4])
		if code == 0 {
			return nil
		}
		msg, _ := readString(body[4:])
		return fmt.Errorf("SFTP 错误 %d: %s", code, msg)
	}
	return fmt.Errorf("意外的响应类型 %d", typ)
}

// 解析 SFTP 属性（标准 v3 flags: size/uidgid/perm/acmodtime）
func parseAttrs(b []byte) (size uint64, perm uint32, mtime string) {
	var flags uint32
	flags, b = readUint32(b)
	if flags&0x1 != 0 {
		size, b = readUint64(b)
	}
	if flags&0x2 != 0 {
		_, b = readUint32(b)
		_, b = readUint32(b)
	}
	if flags&0x4 != 0 {
		perm, b = readUint32(b)
	}
	if flags&0x8 != 0 {
		_, b = readUint32(b)
		m, _ := readUint32(b)
		b = b[4:]
		mtime = time.Unix(int64(m), 0).Format("2006-01-02 15:04:05")
	}
	return
}

func (c *sftpClient) realpath(path string) string {
	typ, body, err := c.call(16, putString(nil, path)) // SSH_FXP_REALPATH
	if err != nil {
		return path
	}
	if typ == 104 { // NAME
		_, body = readUint32(body)
		name, _ := readString(body)
		return name
	}
	return path
}

func (c *sftpClient) opendir(path string) (string, error) {
	typ, body, err := c.call(11, putString(nil, path)) // SSH_FXP_OPENDIR
	if err != nil {
		return "", err
	}
	if typ == 102 { // HANDLE
		h, _ := readString(body)
		return h, nil
	}
	return "", expectStatus(typ, body)
}

func (c *sftpClient) closeHandle(h string) error {
	typ, body, err := c.call(4, putString(nil, h)) // SSH_FXP_CLOSE
	if err != nil {
		return err
	}
	return expectStatus(typ, body)
}

func (c *sftpClient) openFile(path string, flags uint32) (string, error) {
	payload := putString(nil, path)
	payload = append(payload, byte(flags>>24), byte(flags>>16), byte(flags>>8), byte(flags))
	payload = append(payload, 0, 0, 0, 0) // 空属性
	typ, body, err := c.call(3, payload)  // SSH_FXP_OPEN
	if err != nil {
		return "", err
	}
	if typ == 102 {
		h, _ := readString(body)
		return h, nil
	}
	return "", expectStatus(typ, body)
}

func (c *sftpClient) listDir(path string) ([]FileInfo, error) {
	rp := c.realpath(path)
	handle, err := c.opendir(rp)
	if err != nil {
		return nil, err
	}
	defer c.closeHandle(handle)
	out := []FileInfo{}
	for {
		typ, body, err := c.call(12, putString(nil, handle)) // SSH_FXP_READDIR
		if err != nil {
			return nil, err
		}
		if typ == 101 { // STATUS: 正常结束(EOF) 或错误
			code := binary.BigEndian.Uint32(body[:4])
			if code != 0 && code != 1 {
				return nil, expectStatus(typ, body)
			}
			break
		}
		if typ != 104 {
			return nil, fmt.Errorf("READDIR 返回类型 %d", typ)
		}
		count, body := readUint32(body)
		for i := uint32(0); i < count; i++ {
			var name string
			name, body = readString(body)
			_, body = readString(body) // longname
			size, perm, mtime := parseAttrs(body)
			if name == "." || name == ".." {
				continue
			}
			out = append(out, FileInfo{
				Name:  name,
				Path:  joinRemotePath(rp, name),
				IsDir: (perm & 0xF000) == 0x4000,
				Size:  int64(size),
				Mode:  fmt.Sprintf("%o", perm),
				MTime: mtime,
			})
		}
	}
	return out, nil
}

func (c *sftpClient) readFile(path string) (string, error) {
	handle, err := c.openFile(path, 0x1) // READ
	if err != nil {
		return "", err
	}
	defer c.closeHandle(handle)
	var buf bytes.Buffer
	var offset uint64
	const chunkSize = 32768
	for {
		payload := putString(nil, handle)
		payload = append(payload, packUint64(offset)...)
		payload = append(payload, putUint32(uint32(chunkSize))...)
		typ, body, err := c.call(5, payload) // SSH_FXP_READ
		if err != nil {
			return "", err
		}
		if typ == 101 { // EOF / 结束
			break
		}
		if typ != 103 { // DATA
			return "", fmt.Errorf("READ 返回类型 %d", typ)
		}
		data, _ := readString(body)
		if len(data) == 0 {
			break
		}
		buf.WriteString(data)
		offset += uint64(len(data))
		if len(data) < chunkSize {
			break
		}
		if buf.Len() > 5*1024*1024 {
			return "", fmt.Errorf("文件过大（>5MB），仅支持读取文本文件")
		}
	}
	return buf.String(), nil
}

// readFileBytes 以二进制方式读取远端文件（用于下载任意类型文件，最大 200MB）
func (c *sftpClient) readFileBytes(path string) ([]byte, error) {
	handle, err := c.openFile(path, 0x1) // READ
	if err != nil {
		return nil, err
	}
	defer c.closeHandle(handle)
	var buf bytes.Buffer
	var offset uint64
	const chunkSize = 32768
	const maxSize = 200 * 1024 * 1024
	for {
		payload := putString(nil, handle)
		payload = append(payload, packUint64(offset)...)
		payload = append(payload, putUint32(uint32(chunkSize))...)
		typ, body, err := c.call(5, payload) // SSH_FXP_READ
		if err != nil {
			return nil, err
		}
		if typ == 101 { // EOF / 结束
			break
		}
		if typ != 103 { // DATA
			return nil, fmt.Errorf("READ 返回类型 %d", typ)
		}
		data, _ := readString(body)
		if len(data) == 0 {
			break
		}
		buf.WriteString(data)
		offset += uint64(len(data))
		if len(data) < chunkSize {
			break
		}
		if buf.Len() > maxSize {
			return nil, fmt.Errorf("文件过大（>200MB），暂不支持下载")
		}
	}
	return buf.Bytes(), nil
}

// rename 重命名 / 移动远端文件或目录（SSH_FXP_RENAME）
func (c *sftpClient) rename(oldPath, newPath string) error {
	payload := putString(nil, oldPath)
	payload = putString(payload, newPath)
	typ, body, err := c.call(18, payload) // SSH_FXP_RENAME
	if err != nil {
		return err
	}
	return expectStatus(typ, body)
}

func (c *sftpClient) writeFile(path, content string) error {
	handle, err := c.openFile(path, 0x2|0x8|0x10) // WRITE|CREAT|TRUNC
	if err != nil {
		return err
	}
	defer c.closeHandle(handle)
	data := []byte(content)
	var offset uint64
	const chunkSize = 4096
	for len(data) > 0 {
		chunk := data
		if len(chunk) > chunkSize {
			chunk = chunk[:chunkSize]
		}
		payload := putString(nil, handle)
		payload = append(payload, packUint64(offset)...)
		payload = putString(payload, string(chunk))
		typ, body, err := c.call(6, payload) // SSH_FXP_WRITE
		if err != nil {
			return err
		}
		if err := expectStatus(typ, body); err != nil {
			return err
		}
		offset += uint64(len(chunk))
		data = data[len(chunk):]
	}
	return nil
}

// writeFileBytes 以二进制方式写入远端文件（任意字节安全，用于上传图片/压缩包等）
func (c *sftpClient) writeFileBytes(path string, data []byte) error {
	handle, err := c.openFile(path, 0x2|0x8|0x10) // WRITE|CREAT|TRUNC
	if err != nil {
		return err
	}
	defer c.closeHandle(handle)
	var offset uint64
	const chunkSize = 4096
	for len(data) > 0 {
		chunk := data
		if len(chunk) > chunkSize {
			chunk = chunk[:chunkSize]
		}
		payload := putString(nil, handle)
		payload = append(payload, packUint64(offset)...)
		payload = putString(payload, string(chunk))
		typ, body, err := c.call(6, payload) // SSH_FXP_WRITE
		if err != nil {
			return err
		}
		if err := expectStatus(typ, body); err != nil {
			return err
		}
		offset += uint64(len(chunk))
		data = data[len(chunk):]
	}
	return nil
}

func (c *sftpClient) mkdir(path string) error {
	payload := putString(nil, path)
	payload = append(payload, 0, 0, 0, 0)
	typ, body, err := c.call(14, payload) // SSH_FXP_MKDIR
	if err != nil {
		return err
	}
	return expectStatus(typ, body)
}

func (c *sftpClient) stat(path string) (bool, error) {
	typ, body, err := c.call(17, putString(nil, path)) // SSH_FXP_STAT
	if err != nil {
		return false, err
	}
	if typ == 105 { // ATTRS
		_, perm, _ := parseAttrs(body)
		return (perm & 0xF000) == 0x4000, nil
	}
	return false, expectStatus(typ, body)
}

func (c *sftpClient) remove(path string, isDir bool) error {
	pkt := byte(13) // SSH_FXP_REMOVE
	if isDir {
		pkt = 15 // SSH_FXP_RMDIR
	}
	typ, body, err := c.call(pkt, putString(nil, path))
	if err != nil {
		return err
	}
	return expectStatus(typ, body)
}
