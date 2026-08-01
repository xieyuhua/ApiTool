// Package platform 提供 Windows 平台相关的底层能力封装：
//   - 系统剪贴板采集与写入（文本 / 图片 CF_DIB <-> PNG）
//   - 全局键盘钩子（连续两次 Ctrl、Ctrl+B）
//
// 抽离自根目录 clipboard_windows.go / hotkey_windows.go。业务侧（历史存储、窗口控制、
// 前端事件通知）由 App 通过 ClipSink 与 bus.Bus 注入，platform 不反向依赖 App 或 runtime。
package platform

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	cfUnicodeText = 13
	cfDib         = 8  // CF_DIB：设备无关位图
	cfBitmap      = 2  // CF_BITMAP
	gmemMoveable  = 0x0002
)

var (
	clipUser32   = windows.NewLazySystemDLL("user32.dll")
	clipKernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procOpenClipboard      = clipUser32.NewProc("OpenClipboard")
	procCloseClipboard     = clipUser32.NewProc("CloseClipboard")
	procEmptyClipboard     = clipUser32.NewProc("EmptyClipboard")
	procGetClipboardData   = clipUser32.NewProc("GetClipboardData")
	procSetClipboardData   = clipUser32.NewProc("SetClipboardData")
	procIsClipboardFmtAvail = clipUser32.NewProc("IsClipboardFormatAvailable")
	procGetClipboardOwner  = clipUser32.NewProc("GetClipboardOwner")
	procSetClipboardOwner  = clipUser32.NewProc("SetClipboardOwner") // 不存在则忽略

	procGlobalAlloc = clipKernel32.NewProc("GlobalAlloc")
	procGlobalLock  = clipKernel32.NewProc("GlobalLock")
	procGlobalUnlock = clipKernel32.NewProc("GlobalUnlock")
	procGlobalSize  = clipKernel32.NewProc("GlobalSize")
	procGlobalFree  = clipKernel32.NewProc("GlobalFree")
)

// ClipSink 剪贴板采集结果的业务接收方（由 App 实现：负责落盘、去重、裁剪、通知前端）。
type ClipSink interface {
	SaveClipText(text, sig string) error
	SaveClipImage(pngData []byte, w, h int, sig string) error
	NotifyUpdated()
}

// captureState 采集去重状态（记录上一次文本/图片指纹，避免重复写入）
type captureStateT struct {
	mu          sync.Mutex
	lastTextSig string
	lastImgSig  string
	running     bool
	stop        chan struct{}
}

var captureState captureStateT

// StartCapture 启动后台采集（在 startup 中调用一次）。sink 用于把采集结果回写业务层。
func StartCapture(sink ClipSink) {
	captureState.mu.Lock()
	if captureState.running {
		captureState.mu.Unlock()
		return
	}
	captureState.running = true
	captureState.stop = make(chan struct{})
	captureState.mu.Unlock()

	go func() {
		ticker := time.NewTicker(800 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-captureState.stop:
				return
			case <-ticker.C:
				pollClipboard(sink)
			}
		}
	}()
}

// StopCapture 停止后台采集
func StopCapture() {
	captureState.mu.Lock()
	defer captureState.mu.Unlock()
	if captureState.running {
		close(captureState.stop)
		captureState.running = false
	}
}

// MarkWritten 主动标记某次写入（复制历史项 / 写回）的指纹，避免采集器立刻又读回来。
func MarkWritten(textSig, imgSig string) {
	captureState.mu.Lock()
	defer captureState.mu.Unlock()
	if textSig != "" {
		captureState.lastTextSig = textSig
		captureState.lastImgSig = ""
	}
	if imgSig != "" {
		captureState.lastImgSig = imgSig
		captureState.lastTextSig = ""
	}
}

// pollClipboard 读取系统剪贴板当前内容，若与上次不同则通过 sink 记录
func pollClipboard(sink ClipSink) {
	if data, w, h, sig, ok := ReadClipboardImage(); ok {
		captureState.mu.Lock()
		dup := sig == captureState.lastImgSig
		captureState.mu.Unlock()
		if !dup {
			if err := sink.SaveClipImage(data, w, h, sig); err == nil {
				captureState.mu.Lock()
				captureState.lastImgSig = sig
				captureState.lastTextSig = "" // 图片与文本互斥
				captureState.mu.Unlock()
				sink.NotifyUpdated()
			}
		}
		return
	}
	if text, sig, ok := ReadClipboardText(); ok && text != "" {
		captureState.mu.Lock()
		dup := sig == captureState.lastTextSig
		captureState.mu.Unlock()
		if !dup {
			if err := sink.SaveClipText(text, sig); err == nil {
				captureState.mu.Lock()
				captureState.lastTextSig = sig
				captureState.lastImgSig = ""
				captureState.mu.Unlock()
				sink.NotifyUpdated()
			}
		}
	}
}

// ----- 系统剪贴板读取（Win32）-----

func openClipboard() bool {
	r, _, _ := procOpenClipboard.Call(0)
	return r != 0
}

func closeClipboard() {
	procCloseClipboard.Call()
}

func isClipboardFormatAvailable(fmt uint) bool {
	r, _, _ := procIsClipboardFmtAvail.Call(uintptr(fmt))
	return r != 0
}

// ReadClipboardText 读取系统剪贴板文本，返回内容与指纹
func ReadClipboardText() (text string, sig string, ok bool) {
	if !openClipboard() {
		return "", "", false
	}
	defer closeClipboard()
	if !isClipboardFormatAvailable(cfUnicodeText) {
		return "", "", false
	}
	h, _, _ := procGetClipboardData.Call(uintptr(cfUnicodeText))
	if h == 0 {
		return "", "", false
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return "", "", false
	}
	defer procGlobalUnlock.Call(h)

	var runes []uint16
	for i := 0; ; i++ {
		ch := *(*uint16)(unsafe.Pointer(uintptr(p) + uintptr(i)*2))
		if ch == 0 {
			break
		}
		runes = append(runes, ch)
	}
	if len(runes) == 0 {
		return "", "", false
	}
	s := windows.UTF16ToString(runes)
	sig = fmt.Sprintf("%x", sha256.Sum256([]byte(s)))
	return s, sig, true
}

// ReadClipboardImage 读取 CF_DIB 并转为 PNG 字节，返回尺寸与指纹
func ReadClipboardImage() (pngData []byte, w int, h int, sig string, ok bool) {
	if !openClipboard() {
		return nil, 0, 0, "", false
	}
	defer closeClipboard()
	if !isClipboardFormatAvailable(cfDib) {
		return nil, 0, 0, "", false
	}
	hh, _, _ := procGetClipboardData.Call(uintptr(cfDib))
	if hh == 0 {
		return nil, 0, 0, "", false
	}
	size, _, _ := procGlobalSize.Call(hh)
	if size == 0 {
		return nil, 0, 0, "", false
	}
	p, _, _ := procGlobalLock.Call(hh)
	if p == 0 {
		return nil, 0, 0, "", false
	}
	defer procGlobalUnlock.Call(hh)

	dib := make([]byte, size)
	copy(dib, (*(*[1 << 30]byte)(unsafe.Pointer(p)))[:size:size])

	img, err := dibToImage(dib)
	if err != nil {
		return nil, 0, 0, "", false
	}
	bounds := img.Bounds()
	w, h = bounds.Dx(), bounds.Dy()

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, 0, 0, "", false
	}
	pngBytes := buf.Bytes()
	sig = fmt.Sprintf("%x", sha256.Sum256(pngBytes))
	return pngBytes, w, h, sig, true
}

// dibToImage 将 CF_DIB 内存（BITMAPINFOHEADER + 像素数据）解码为 image.Image。
func dibToImage(dib []byte) (image.Image, error) {
	if len(dib) < 40 {
		return nil, fmt.Errorf("DIB 数据过短")
	}
	biSize := uint32(binaryLittleEndianUint32(dib[0:4]))
	if biSize < 40 {
		return nil, fmt.Errorf("不支持的 BITMAPINFOHEADER 大小")
	}
	width := int(int32(binaryLittleEndianUint32(dib[4:8])))
	height := int(int32(binaryLittleEndianUint32(dib[8:12])))
	if width <= 0 || height == 0 {
		return nil, fmt.Errorf("非法尺寸")
	}
	topDown := height < 0
	if topDown {
		height = -height
	}
	bitCount := int(uint16(binaryLittleEndianUint16(dib[14:16])))
	if bitCount != 24 && bitCount != 32 {
		return nil, fmt.Errorf("不支持的位深 %d", bitCount)
	}
	dataOffset := int(biSize)
	if len(dib) < dataOffset {
		return nil, fmt.Errorf("DIB 缺少像素数据")
	}
	pixels := dib[dataOffset:]

	bytesPerPixel := bitCount / 8
	rowSize := (width*bytesPerPixel + 3) &^ 3
	expected := rowSize * height
	if len(pixels) < expected {
		height = len(pixels) / rowSize
		if height <= 0 {
			return nil, fmt.Errorf("像素数据不足")
		}
		expected = rowSize * height
	}

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		srcRow := y
		if !topDown {
			srcRow = height - 1 - y
		}
		srcOff := srcRow * rowSize
		dstOff := y * width * 4
		for x := 0; x < width; x++ {
			o := srcOff + x*bytesPerPixel
			var r, g, b, a uint8
			if bytesPerPixel == 4 {
				b = pixels[o]
				g = pixels[o+1]
				r = pixels[o+2]
				a = pixels[o+3]
				if a == 0 {
					a = 255
				}
			} else {
				b = pixels[o]
				g = pixels[o+1]
				r = pixels[o+2]
				a = 255
			}
			idx := dstOff + x*4
			img.Pix[idx] = r
			img.Pix[idx+1] = g
			img.Pix[idx+2] = b
			img.Pix[idx+3] = a
		}
	}
	return img, nil
}

// ----- 剪贴板写入（DIB）-----

// WriteClipboardImagePNG 将 PNG 字节转换并写入系统剪贴板（CF_DIB）。
// 文本写入由业务层通过 bus.Bus.ClipboardSetText 完成（Wails runtime 提供跨平台能力）。
func WriteClipboardImagePNG(pngBytes []byte) error {
	dib, err := pngToDIB(pngBytes)
	if err != nil {
		return err
	}
	return writeClipboardDIB(dib)
}

// pngToDIB 将 PNG 字节转换为 BITMAPINFOHEADER + 像素（BGRA，自下而上）
func pngToDIB(pngBytes []byte) ([]byte, error) {
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return nil, err
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w == 0 || h == 0 {
		return nil, fmt.Errorf("空图片")
	}
	rowSize := (w*4 + 3) &^ 3
	biSize := 40
	total := biSize + rowSize*h
	dib := make([]byte, total)
	putUint32LE(dib[0:4], uint32(biSize))
	putUint32LE(dib[4:8], uint32(w))
	putUint32LE(dib[8:12], uint32(h))
	putUint16LE(dib[12:14], 1)
	putUint16LE(dib[14:16], 32)
	putUint32LE(dib[16:20], uint32(rowSize*h))

	for y := 0; y < h; y++ {
		srcRow := h - 1 - y
		srcOff := srcRow * rowSize
		for x := 0; x < w; x++ {
			c := color.RGBAModel.Convert(img.At(bounds.Min.X+x, bounds.Min.Y+srcRow)).(color.RGBA)
			o := srcOff + x*4
			dib[biSize+o] = c.B
			dib[biSize+o+1] = c.G
			dib[biSize+o+2] = c.R
			dib[biSize+o+3] = c.A
		}
	}
	return dib, nil
}

func writeClipboardDIB(dib []byte) error {
	if !openClipboard() {
		return fmt.Errorf("无法打开剪贴板")
	}
	defer closeClipboard()
	procEmptyClipboard.Call()
	size := uintptr(len(dib))
	h, _, err := procGlobalAlloc.Call(uintptr(gmemMoveable), size)
	if h == 0 {
		return fmt.Errorf("GlobalAlloc 失败")
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("GlobalLock 失败")
	}
	copy((*(*[1 << 30]byte)(unsafe.Pointer(p)))[:size:size], dib)
	procGlobalUnlock.Call(h)

	r, _, err := procSetClipboardData.Call(uintptr(cfDib), h)
	if r == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("SetClipboardData 失败: %v", err)
	}
	return nil
}

// ----- 图片数据读取辅助（供 App 获取缩略图 base64）-----

// ReadPNGFileBase64 读取本地 PNG 文件并返回 base64（用于前端展示缩略图）
func ReadPNGFileBase64(path string) (map[string]string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return map[string]string{
		"mime": "image/png",
		"data": base64.StdEncoding.EncodeToString(b),
	}, true
}

// 小端读写辅助（替代 binary 包以减少 import 噪音）
func binaryLittleEndianUint32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

func binaryLittleEndianUint16(b []byte) uint16 {
	return uint16(b[0]) | uint16(b[1])<<8
}

func putUint32LE(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

func putUint16LE(b []byte, v uint16) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}
