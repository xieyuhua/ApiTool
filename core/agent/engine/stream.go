package engine

// 引擎层：流式输出的增量过滤。
//
// 模型输出的工具调用标签可能被切成多段（一个 <tool_call> 跨多个 chunk），
// 直接边收边发会让前端看到半截标签。bodyFilter 负责缓冲可疑片段，
// 直到能确定它是正文还是工具调用，再决定补发还是丢弃。

import "strings"

// partialTailLen 返回 buf 末尾可能是 marker 前缀的长度（用于跨分片保留半个标记）。
func partialTailLen(buf, marker string) int {
	max := len(marker) - 1
	if max > len(buf) {
		max = len(buf)
	}
	for n := max; n > 0; n-- {
		if strings.HasPrefix(marker, buf[len(buf)-n:]) {
			return n
		}
	}
	return 0
}

// fenceBufMax ```json 块缓冲上限：超过仍无法判定为工具调用时按普通内容补发，
// 避免异常输出把正文一直憋住不显示。
const fenceBufMax = 4096

// bodyFilter 流式正文过滤器。
// 模型输出工具调用时，工具调用本身不应作为正文实时展示——它最终会被 stripTags 剥除，
// 若实时推送，用户会先看到一坨 JSON，下一轮 loop-start 又被清空，产生「回答了两次」的错觉。
// 该过滤器只影响推送给前端的增量，不影响返回给上层的完整文本（完整文本仍需用于解析工具调用）。
type bodyFilter struct {
	suppressed bool // 已确认进入工具调用区段：本轮剩余内容全部丢弃
	inFence    bool // 正在缓冲 ```json 块以判定是否为工具调用
	fenceBuf   strings.Builder
	pending    strings.Builder // 跨分片暂存（不完整的工具调用标记前缀）
	emit       func(string)    // 推送已确认的正文
}

// Write 接收一段正文增量，过滤掉工具调用区段后推送剩余部分。
func (f *bodyFilter) Write(s string) {
	if s == "" {
		return
	}
	// 规范化模型可能输出的 ｜｜DSML｜｜ 前缀标签，使其能被后续标记判定与剥离识别
	s = normalizeToolTags(s)
	// 拼接上一分片留下的不完整标记尾巴
	if f.pending.Len() > 0 {
		s = f.pending.String() + s
		f.pending.Reset()
	}
	for len(s) > 0 {
		if f.suppressed {
			return // 已进入工具调用区段，本轮剩余内容全部丢弃
		}
		if f.inFence {
			// 在 ```json 块内：等块结束（或已能判定）再决定是工具调用还是普通代码
			endIdx := strings.Index(s, "```")
			if endIdx >= 0 {
				f.fenceBuf.WriteString(s[:endIdx])
				s = s[endIdx+3:]
				f.inFence = false
				body := f.fenceBuf.String()
				f.fenceBuf.Reset()
				if toolJSONProbeRe.MatchString(body) {
					f.suppressed = true // 确认是工具调用，丢弃
					return
				}
				// 普通代码块：连同围栏标记一并补发，保证 markdown 结构完整
				f.emit("```json" + body + "```")
				continue
			}
			f.fenceBuf.WriteString(s)
			body := f.fenceBuf.String()
			if toolJSONProbeRe.MatchString(body) {
				f.fenceBuf.Reset()
				f.suppressed = true
				return
			}
			if f.fenceBuf.Len() > fenceBufMax {
				// 超长仍无法判定：按普通内容补发，避免吞掉正文
				f.fenceBuf.Reset()
				f.inFence = false
				f.emit("```json" + body)
			}
			return
		}
		// 查找最近的工具调用起始标记
		cut, cutTag, cutHit := -1, "", false
		for _, m := range toolCallMarkers {
			if i := strings.Index(s, m.tag); i >= 0 && (cut < 0 || i < cut) {
				cut, cutTag, cutHit = i, m.tag, m.hit
			}
		}
		if cut >= 0 {
			f.emit(s[:cut]) // 标记之前的正文正常推送
			s = s[cut+len(cutTag):]
			if cutHit {
				f.suppressed = true // 标签形式：确认是工具调用
				return
			}
			f.inFence = true // ```json：进入缓冲判定
			f.fenceBuf.Reset()
			continue
		}
		// 可能是被截断的标记前缀，留给下一个分片判定
		if keep := toolCallPartialTail(s); keep > 0 {
			f.emit(s[:len(s)-keep])
			f.pending.WriteString(s[len(s)-keep:])
			return
		}
		f.emit(s)
		return
	}
}

// Flush 流结束时输出残留：未闭合的 ```json 缓冲按普通内容补发，避免丢失正文。
func (f *bodyFilter) Flush() {
	if f.suppressed {
		return
	}
	if f.pending.Len() > 0 {
		f.emit(f.pending.String())
		f.pending.Reset()
	}
	if f.inFence && f.fenceBuf.Len() > 0 {
		f.emit("```json" + f.fenceBuf.String())
		f.fenceBuf.Reset()
		f.inFence = false
	}
}
