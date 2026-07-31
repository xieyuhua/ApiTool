package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"
)

// ---------------------------------------------------------------------------
// 剪贴板采集与历史管理（Windows）：同时支持文本与图片（CF_DIB -> PNG）。
// 采集在 Go 端轮询系统剪贴板，写入 data.json，并通过事件通知前端刷新。
// 前端「剪贴板历史」窗口通过 GetClipItems 拉取、CopyClipItem 复制、DeleteClipItem 删除。
// ---------------------------------------------------------------------------

const (
	cfUnicodeText = 13
	cfDib         = 8  // CF_DIB：设备无关位图
	cfBitmap      = 2  // CF_BITMAP
	gmemMoveable  = 0x0002
)

var (
	clipUser32   = windows.NewLazySystemDLL("user32.dll")
	clipKernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procOpenClipboard   = clipUser32.NewProc("OpenClipboard")
	procCloseClipboard  = clipUser32.NewProc("CloseClipboard")
	procEmptyClipboard  = clipUser32.NewProc("EmptyClipboard")
	procGetClipboardData = clipUser32.NewProc("GetClipboardData")
	procSetClipboardData = clipUser32.NewProc("SetClipboardData")
	procIsClipboardFmtAvail = clipUser32.NewProc("IsClipboardFormatAvailable")
	procGetClipboardOwner = clipUser32.NewProc("GetClipboardOwner")
	procSetClipboardOwner = clipUser32.NewProc("SetClipboardOwner") // 不存在则忽略

	procGlobalAlloc = clipKernel32.NewProc("GlobalAlloc")
	procGlobalLock  = clipKernel32.NewProc("GlobalLock")
	procGlobalUnlock = clipKernel32.NewProc("GlobalUnlock")
	procGlobalSize  = clipKernel32.NewProc("GlobalSize")
	procGlobalFree  = clipKernel32.NewProc("GlobalFree")
)

// 采集去重：记录上一次文本/图片指纹，避免重复写入
type clipCaptureState struct {
	mu          sync.Mutex
	lastTextSig string
	lastImgSig  string
	running     bool
	stop        chan struct{}
}

var captureState clipCaptureState

// StartClipboardCapture 启动后台采集（在 startup 中调用一次）
func (a *App) StartClipboardCapture() {
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
				a.pollClipboard()
			}
		}
	}()
}

// StopClipboardCapture 停止后台采集
func (a *App) StopClipboardCapture() {
	captureState.mu.Lock()
	defer captureState.mu.Unlock()
	if captureState.running {
		close(captureState.stop)
		captureState.running = false
	}
}

// pollClipboard 读取系统剪贴板当前内容，若与上次不同则记录
func (a *App) pollClipboard() {
	// 避免读取自己写入剪贴板后立刻又采集回来（复制历史项时）
	// 优先尝试图片（DIB），再尝试文本
	if data, w, h, sig, ok := readClipboardImage(); ok {
		captureState.mu.Lock()
		dup := sig == captureState.lastImgSig
		captureState.mu.Unlock()
		if !dup {
			if err := a.saveClipImage(data, w, h, sig); err == nil {
				captureState.mu.Lock()
				captureState.lastImgSig = sig
				captureState.lastTextSig = "" // 图片与文本互斥
				captureState.mu.Unlock()
				a.notifyClipboardUpdated()
			}
		}
		return
	}

	if text, sig, ok := readClipboardText(); ok && text != "" {
		captureState.mu.Lock()
		dup := sig == captureState.lastTextSig
		captureState.mu.Unlock()
		if !dup {
			if err := a.saveClipText(text, sig); err == nil {
				captureState.mu.Lock()
				captureState.lastTextSig = sig
				captureState.lastImgSig = ""
				captureState.mu.Unlock()
				a.notifyClipboardUpdated()
			}
		}
	}
}

func (a *App) notifyClipboardUpdated() {
	runtime.EventsEmit(a.ctx, "apitool:clipboard-updated")
}

// ----- 系统剪贴板读取（Win32）-----

func openClipboard() bool {
	// OpenClipboard 参数为 HWND，传 NULL(0)
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

func readClipboardText() (text string, sig string, ok bool) {
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

	// UTF-16 以 0 结尾
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

// readClipboardImage 读取 CF_DIB 并转为 PNG 字节，返回尺寸与指纹
func readClipboardImage() (pngData []byte, w int, h int, sig string, ok bool) {
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
// 支持 24/32 位、自下而上的常见剪贴板位图；其它格式回退返回错误。
func dibToImage(dib []byte) (image.Image, error) {
	if len(dib) < 40 {
		return nil, fmt.Errorf("DIB 数据过短")
	}
	// BITMAPINFOHEADER
	biSize := binary.LittleEndian.Uint32(dib[0:4])
	if biSize < 40 {
		return nil, fmt.Errorf("不支持的 BITMAPINFOHEADER 大小")
	}
	width := int(int32(binary.LittleEndian.Uint32(dib[4:8])))
	height := int(int32(binary.LittleEndian.Uint32(dib[8:12])))
	if width <= 0 || height == 0 {
		return nil, fmt.Errorf("非法尺寸")
	}
	// 正值表示自下而上；负值表示自上而下（少见）
	topDown := height < 0
	if topDown {
		height = -height
	}
	bitCount := int(binary.LittleEndian.Uint16(dib[14:16]))
	if bitCount != 24 && bitCount != 32 {
		return nil, fmt.Errorf("不支持的位深 %d", bitCount)
	}

	// 像素数据起始：biSize + 调色板（24/32 位无调色板）
	dataOffset := int(biSize)
	if len(dib) < dataOffset {
		return nil, fmt.Errorf("DIB 缺少像素数据")
	}
	pixels := dib[dataOffset:]

	bytesPerPixel := bitCount / 8
	rowSize := (width*bytesPerPixel + 3) &^ 3 // 4 字节对齐
	expected := rowSize * height
	if len(pixels) < expected {
		// 允许部分数据：截断到可用行数
		height = len(pixels) / rowSize
		if height <= 0 {
			return nil, fmt.Errorf("像素数据不足")
		}
		expected = rowSize * height
	}

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		// 自下而上：第 0 行数据在底部
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
				// 若 alpha 全 0 视为不透明（部分程序未设置 alpha）
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

// ----- 落盘存储 -----

// imgDir 返回图片存储目录（dataDir/clipimg）
func (a *App) imgDir() string {
	dir := filepath.Join(filepath.Dir(a.dataFile), "clipimg")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// saveClipText 记录一条文本剪贴板
func (a *App) saveClipText(text string, sig string) error {
	data := a.LoadData()
	item := ClipItem{
		ID:        genClipID(),
		Type:      ClipTypeText,
		Text:      text,
		Time:      time.Now().Format("2006-01-02 15:04:05"),
		Timestamp: time.Now().UnixMilli(),
	}
	a.pushClip(&data, item)
	return a.SaveData(data)
}

// saveClipImage 保存 PNG 字节到磁盘并记录一条图片剪贴板
func (a *App) saveClipImage(pngData []byte, w, h int, sig string) error {
	id := genClipID()
	rel := filepath.Join("clipimg", id+".png")
	full := filepath.Join(filepath.Dir(a.dataFile), rel)
	if err := os.WriteFile(full, pngData, 0o644); err != nil {
		return err
	}
	data := a.LoadData()
	item := ClipItem{
		ID:        id,
		Type:      ClipTypeImage,
		ImagePath: rel,
		Width:     w,
		Height:    h,
		Time:      time.Now().Format("2006-01-02 15:04:05"),
		Timestamp: time.Now().UnixMilli(),
	}
	a.pushClip(&data, item)
	return a.SaveData(data)
}

// pushClip 将条目插入历史最前，并按上限裁剪（上限来自设置）
func (a *App) pushClip(data *AppData, item ClipItem) {
	// 同类去重：完全一致的文本/图片指纹无需重复
	for i, it := range data.Clipboard.History {
		if it.Type == item.Type {
			if item.Type == ClipTypeText && it.Text == item.Text {
				// 移到最前
				data.Clipboard.History = append(data.Clipboard.History[:i:i], data.Clipboard.History[i+1:]...)
				break
			}
		}
	}
	data.Clipboard.History = append([]ClipItem{item}, data.Clipboard.History...)
	maxItems := data.Settings.Clipboard.MaxItems
	if maxItems <= 0 {
		maxItems = 200
	}
	if len(data.Clipboard.History) > maxItems {
		// 删除超出部分的图片文件
		for _, it := range data.Clipboard.History[maxItems:] {
			if it.Type == ClipTypeImage && it.ImagePath != "" {
				_ = os.Remove(filepath.Join(filepath.Dir(a.dataFile), it.ImagePath))
			}
		}
		data.Clipboard.History = data.Clipboard.History[:maxItems]
	}
}

func genClipID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// ----- 对外暴露给前端的 API -----

// GetClipItems 返回剪贴板历史（最新在前）
func (a *App) GetClipItems() []ClipItem {
	data := a.LoadData()
	return data.Clipboard.History
}

// CopyClipItem 复制指定历史项到系统剪贴板。
// 文本直接写入；图片读取本地 PNG 并写回剪贴板（CF_DIB）。
func (a *App) CopyClipItem(id string) error {
	data := a.LoadData()
	var item *ClipItem
	for i := range data.Clipboard.History {
		if data.Clipboard.History[i].ID == id {
			item = &data.Clipboard.History[i]
			break
		}
	}
	if item == nil {
		return fmt.Errorf("未找到该记录")
	}
	if item.Type == ClipTypeText {
		// 写入后更新去重指纹，避免立刻又被采集回来
		sig := fmt.Sprintf("%x", sha256.Sum256([]byte(item.Text)))
		captureState.mu.Lock()
		captureState.lastTextSig = sig
		captureState.lastImgSig = ""
		captureState.mu.Unlock()
		return runtime.ClipboardSetText(a.ctx, item.Text)
	}
	// 图片：读取 PNG，转 DIB 写回剪贴板
	full := filepath.Join(filepath.Dir(a.dataFile), item.ImagePath)
	f, err := os.Open(full)
	if err != nil {
		return err
	}
	defer f.Close()
	pngBytes, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	dib, err := pngToDIB(pngBytes)
	if err != nil {
		return err
	}
	sig := fmt.Sprintf("%x", sha256.Sum256(pngBytes))
	captureState.mu.Lock()
	captureState.lastImgSig = sig
	captureState.lastTextSig = ""
	captureState.mu.Unlock()
	return writeClipboardDIB(dib)
}

// GetClipImageData 返回指定图片历史项的 PNG 数据（base64），用于前端直接展示缩略图。
func (a *App) GetClipImageData(id string) map[string]string {
	data := a.LoadData()
	for _, it := range data.Clipboard.History {
		if it.ID == id && it.Type == ClipTypeImage && it.ImagePath != "" {
			full := filepath.Join(filepath.Dir(a.dataFile), it.ImagePath)
			b, err := os.ReadFile(full)
			if err == nil {
				return map[string]string{
					"mime": "image/png",
					"data": base64.StdEncoding.EncodeToString(b),
				}
			}
		}
	}
	return map[string]string{}
}

// DeleteClipItem 删除一条历史记录（同时删除图片文件）
func (a *App) DeleteClipItem(id string) error {
	data := a.LoadData()
	for i, it := range data.Clipboard.History {
		if it.ID == id {
			if it.Type == ClipTypeImage && it.ImagePath != "" {
				_ = os.Remove(filepath.Join(filepath.Dir(a.dataFile), it.ImagePath))
			}
			data.Clipboard.History = append(data.Clipboard.History[:i], data.Clipboard.History[i+1:]...)
			break
		}
	}
	return a.SaveData(data)
}

// ----- 剪贴板历史窗口控制 -----
// 语义：剪贴板历史是一个「独立弹出浮层」。打开时显示主窗口并置顶居中；
// 关闭时取消置顶并把窗口隐藏回托盘。全程以 clipWinVisible 为唯一状态机，
// 不再依赖 windowVisible，避免状态错位导致「只能打开一次」。

// toggleClipboardWindow 切换剪贴板历史浮层的显隐
func (a *App) toggleClipboardWindow() {
	if a.clipWinVisible {
		a.CloseClipboardWindow()
	} else {
		a.ShowClipboardWindow()
	}
}

// ShowClipboardWindow 显示剪贴板历史浮层（复用主窗口，置顶居中）
func (a *App) ShowClipboardWindow() {
	a.clipWinVisible = true
	runtime.WindowShow(a.ctx)
	runtime.WindowUnminimise(a.ctx)
	runtime.WindowSetAlwaysOnTop(a.ctx, true)
	runtime.WindowCenter(a.ctx)
	runtime.EventsEmit(a.ctx, "apitool:show-clipboard-history")
}

// CloseClipboardWindow 关闭剪贴板历史浮层（取消置顶并隐藏窗口回托盘）
func (a *App) CloseClipboardWindow() {
	a.clipWinVisible = false
	runtime.WindowSetAlwaysOnTop(a.ctx, false)
	runtime.EventsEmit(a.ctx, "apitool:hide-clipboard-history")
	runtime.WindowHide(a.ctx)
}

// ----- 剪贴板写入（DIB）-----

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
	// 32 位 BGRA，自下而上
	rowSize := (w*4 + 3) &^ 3
	biSize := 40
	total := biSize + rowSize*h
	dib := make([]byte, total)
	// BITMAPINFOHEADER
	binary.LittleEndian.PutUint32(dib[0:4], uint32(biSize))
	binary.LittleEndian.PutUint32(dib[4:8], uint32(w))
	binary.LittleEndian.PutUint32(dib[8:12], uint32(h))
	binary.LittleEndian.PutUint16(dib[12:14], 1) // planes
	binary.LittleEndian.PutUint16(dib[14:16], 32)
	// biCompression = 0 (BI_RGB)
	binary.LittleEndian.PutUint32(dib[16:20], uint32(rowSize*h))
	// 其余字段默认 0

	for y := 0; y < h; y++ {
		// 自下而上：目标第 srcRow = h-1-y
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

// writeClipboardDIB 将 DIB 写入系统剪贴板（CF_DIB）
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

// ensureJSONField helper: 兼容旧版 data.json 缺少 clipboard 字段时的解析。
// 已在 readData 中通过 defaultData 兜底，此处无需额外处理。

var _ = json.Marshal
