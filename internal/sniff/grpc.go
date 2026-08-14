package sniff

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// grpc 解码：将 gRPC over HTTP/2 的 protobuf 帧解析为人类可读文本。
//
// 设计取舍：
//   - protobuf 是二进制、自描述字段号 + 线型的格式，但字段的语义（尤其是嵌套 message
//     与 string/bytes 的区分）需要 .proto 描述符才能精确还原。
//   - 这里实现「零依赖」的通用 wire 解析：遍历字段号与类型，把字符串/可打印内容直接
//     展示，嵌套结构递归展开，数值按 int/uint/float 呈现。即便没有 .proto，也能还原出
//     绝大多数请求/响应内容（对含可读字符串字段的服务尤其有效）。
//   - 若后续用户提供 .proto 文件，可在此之上叠加「精确解码」（按描述符解析字段名）。

const (
	grpcCompressedFlag = 0x01
	maxGRPCMessages    = 32     // 单流最多解析的 message 数，避免异常数据导致死循环
	maxProtoDepth      = 32     // protobuf 嵌套最大深度
	maxProtoBytes      = 16 << 20 // 单 message 最大解析字节（16MB），超出按十六进制截断
)

// decodeGRPCBody 解析 gRPC 消息体（可能串联多个 message 帧）。
// 返回格式化后的可读文本；无法解析时返回带原始 hex 的提示。
func decodeGRPCBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var messages []interface{}
	off := 0
	count := 0
	for off < len(body) {
		if len(body)-off < 5 {
			// 余数不足一个帧头：可能是尾部填充/分片，停止解析
			break
		}
		compressed := body[off] & grpcCompressedFlag
		_ = compressed // gRPC 压缩（gzip）标志；当前仅展示原始 payload，未解压
		length := binary.BigEndian.Uint32(body[off+1 : off+5])
		off += 5
		if int(length) > len(body)-off {
			// 长度越界：数据不完整或不是标准 gRPC 帧，退化为整段解析一次
			break
		}
		payload := body[off : off+int(length)]
		off += int(length)
		count++
		if count > maxGRPCMessages {
			messages = append(messages, fmt.Sprintf("<truncated: 超过 %d 条 message>", maxGRPCMessages))
			break
		}
		msg, consumed := parseProtoMessage(payload, 0, 0)
		_ = consumed
		messages = append(messages, msg)
	}

	if len(messages) == 0 {
		// 没有解析出标准 message 帧：可能是非标准封装，退化为整段一次性解析
		msg, _ := parseProtoMessage(body, 0, 0)
		messages = append(messages, msg)
	}
	if len(messages) == 1 {
		return marshalProtoValue(messages[0])
	}
	return marshalProtoValue(messages)
}

// parseProtoMessage 通用 protobuf wire 解析。
// 返回 fieldNumber -> 解码值的 map；值为以下之一：
//   - string（UTF-8 可打印）
//   - []byte 的 hex 字符串（不可打印的 bytes）
//   - map[uint64]interface{}（嵌套 message）
//   - int64 / uint64 / bool / float64
func parseProtoMessage(buf []byte, depth, start int) (map[uint64]interface{}, int) {
	out := map[uint64]interface{}{}
	pos := start
	if depth > maxProtoDepth {
		return out, pos
	}
	for pos < len(buf) {
		tag, n := readVarint(buf, pos)
		if n == 0 {
			break
		}
		pos += n
		fieldNum := tag >> 3
		wireType := tag & 0x7
		switch wireType {
		case 0: // varint
			v, m := readVarint(buf, pos)
			if m == 0 {
				return out, pos
			}
			pos += m
			if v == 0 || v == 1 {
				out[fieldNum] = (v == 1) // 启发式：0/1 视为 bool
			} else {
				out[fieldNum] = int64(v)
			}
		case 1: // 64-bit
			if pos+8 > len(buf) {
				return out, pos
			}
			out[fieldNum] = float64(int64(binary.LittleEndian.Uint64(buf[pos : pos+8])))
			pos += 8
		case 2: // length-delimited
			l, m := readVarint(buf, pos)
			if m == 0 {
				return out, pos
			}
			pos += m
			if int(l) > len(buf)-pos || int(l) > maxProtoBytes {
				out[fieldNum] = hex.EncodeToString(buf[pos:minInt(len(buf), pos+int(l))])
				if int(l) <= len(buf)-pos {
					pos += int(l)
				}
				break
			}
			data := buf[pos : pos+int(l)]
			pos += int(l)
			out[fieldNum] = decodeLengthDelimited(data, depth)
		case 5: // 32-bit
			if pos+4 > len(buf) {
				return out, pos
			}
			out[fieldNum] = float64(int64(binary.LittleEndian.Uint32(buf[pos : pos+4])))
			pos += 4
		default:
			// 未知线型：停止，避免误读
			return out, pos
		}
	}
	return out, pos
}

// decodeLengthDelimited 处理 length-delimited 字段：优先按 UTF-8 字符串，
// 否则尝试递归解析为嵌套 message，最后回退 hex。
func decodeLengthDelimited(data []byte, depth int) interface{} {
	if len(data) == 0 {
		return ""
	}
	// 启发式：若整段都是可打印 UTF-8，视为字符串
	if looksLikeText(data) {
		if s, ok := tryUTF8(data); ok {
			return s
		}
	}
	// 尝试作为嵌套 message 解析（成功且消耗了大部分字节则采用）
	if nested, consumed := parseProtoMessage(data, depth+1, 0); len(nested) > 0 && consumed >= len(data)-1 {
		return nested
	}
	return hex.EncodeToString(data)
}

// looksLikeText 判断字节是否整体像文本（可打印 ASCII / UTF-8，且控制字符占比低）。
func looksLikeText(b []byte) bool {
	if !utf8.Valid(b) {
		return false
	}
	control := 0
	for _, c := range b {
		if c == '\t' || c == '\n' || c == '\r' {
			continue
		}
		if c < 0x20 || c == 0x7f {
			control++
		}
	}
	return control == 0 || float64(control)/float64(len(b)) < 0.05
}

// tryUTF8 尝试将字节解析为 UTF-8 字符串。
func tryUTF8(b []byte) (string, bool) {
	if !utf8.Valid(b) {
		return "", false
	}
	return string(b), true
}

// readVarint 读取 protobuf 变长整数（varint），返回值与消耗字节数（0 表示失败）。
func readVarint(buf []byte, pos int) (uint64, int) {
	var result uint64
	var shift uint
	for i := pos; i < len(buf); i++ {
		b := buf[i]
		result |= uint64(b&0x7f) << shift
		if b&0x80 == 0 {
			return result, i - pos + 1
		}
		shift += 7
		if shift >= 64 {
			return 0, 0
		}
	}
	return 0, 0
}

// marshalProtoValue 将解析结果序列化为缩进 JSON 文本。
func marshalProtoValue(v interface{}) string {
	// 把 map[uint64]interface{} 转成 map[string]interface{} 以便 JSON key 为字段号
	converted := convertProtoForJSON(v)
	b, err := json.MarshalIndent(converted, "", "  ")
	if err != nil {
		return fmt.Sprintf("<grpc decode error: %v>", err)
	}
	return string(b)
}

// convertProtoForJSON 将 protobuf 字段号 map 递归转为字符串 key，便于 JSON 展示。
func convertProtoForJSON(v interface{}) interface{} {
	switch t := v.(type) {
	case map[uint64]interface{}:
		m := map[string]interface{}{}
		for k, val := range t {
			m[fmt.Sprintf("field_%d", k)] = convertProtoForJSON(val)
		}
		return m
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, e := range t {
			out[i] = convertProtoForJSON(e)
		}
		return out
	default:
		return t
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// grpcSummary 生成 gRPC 流量的简短摘要（用于列表预览 / 调试），避免重复逻辑。
func grpcSummary(body []byte) string {
	s := decodeGRPCBody(body)
	if len(s) > 200 {
		s = strings.TrimSpace(s[:200]) + " …"
	}
	return s
}
