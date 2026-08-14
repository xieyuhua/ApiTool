package sniff

import (
	"encoding/binary"
	"testing"
)

// buildGRPCFrame 构造一个标准 gRPC 帧（compressed=0 + 4 字节大端长度 + payload）。
func buildGRPCFrame(payload []byte) []byte {
	frame := make([]byte, 5+len(payload))
	frame[0] = 0
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(payload)))
	copy(frame[5:], payload)
	return frame
}

// encodeVarint 简单 varint 编码（仅用于测试）。
func encodeVarint(v uint64) []byte {
	var buf []byte
	for v >= 0x80 {
		buf = append(buf, byte(v)|0x80)
		v >>= 7
	}
	buf = append(buf, byte(v))
	return buf
}

// makeStringField 构造一个 string 类型的 length-delimited 字段。
func makeStringField(fieldNum uint64, s string) []byte {
	tag := (fieldNum << 3) | 2
	data := []byte(s)
	var out []byte
	out = append(out, encodeVarint(tag)...)
	out = append(out, encodeVarint(uint64(len(data)))...)
	out = append(out, data...)
	return out
}

// makeVarintField 构造一个 varint 字段。
func makeVarintField(fieldNum uint64, v uint64) []byte {
	tag := (fieldNum << 3) | 0
	var out []byte
	out = append(out, encodeVarint(tag)...)
	out = append(out, encodeVarint(v)...)
	return out
}

func TestDecodeGRPCBody(t *testing.T) {
	// message: field 1 = "hello", field 2 = 42
	payload := append(makeStringField(1, "hello"), makeVarintField(2, 42)...)
	body := buildGRPCFrame(payload)

	out := decodeGRPCBody(body)
	t.Logf("decoded:\n%s", out)
	if out == "" {
		t.Fatal("decode returned empty")
	}
	if !contains(out, "hello") {
		t.Errorf("expected 'hello' in output, got:\n%s", out)
	}
	if !contains(out, "field_1") || !contains(out, "field_2") {
		t.Errorf("expected field_1/field_2, got:\n%s", out)
	}
}

func TestDecodeGRPCBodyMultipleMessages(t *testing.T) {
	p1 := makeStringField(1, "first")
	p2 := makeStringField(1, "second")
	body := append(buildGRPCFrame(p1), buildGRPCFrame(p2)...)
	out := decodeGRPCBody(body)
	t.Logf("decoded multi:\n%s", out)
	if !contains(out, "first") || !contains(out, "second") {
		t.Errorf("expected both messages, got:\n%s", out)
	}
}

func TestParseProtoNested(t *testing.T) {
	// 嵌套：field 3 是一个子 message，内含 field 1 = "nested"
	inner := makeStringField(1, "nested")
	tag := (uint64(3) << 3) | 2
	var nested []byte
	nested = append(nested, encodeVarint(tag)...)
	nested = append(nested, encodeVarint(uint64(len(inner)))...)
	nested = append(nested, inner...)
	body := buildGRPCFrame(nested)
	out := decodeGRPCBody(body)
	t.Logf("decoded nested:\n%s", out)
	if !contains(out, "nested") {
		t.Errorf("expected nested string, got:\n%s", out)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
