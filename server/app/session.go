package app

import (
	"fmt"
	"sync"
	"time"

	"apitool/internal/appcore"
	"apitool/internal/bus"
	"apitool/internal/model"
	"apitool/internal/wailsrt"
	"apitool/server/store"
)

// sessionBus 服务端总线：事件按用户分频道推送 SSE；桌面系统能力全部不可用。
type sessionBus struct {
	s      *Server
	userID string
}

// Emit 推送事件到该用户的 SSE 频道。
func (b *sessionBus) Emit(event string, data ...interface{}) {
	b.s.hub.pushUserEvent(b.userID, event, data)
}

func (b *sessionBus) Quit()                       {}
func (b *sessionBus) WindowShow()                 {}
func (b *sessionBus) WindowHide()                 {}
func (b *sessionBus) WindowUnminimise()           {}
func (b *sessionBus) WindowCenter()               {}
func (b *sessionBus) WindowSetAlwaysOnTop(bool)   {}
func (b *sessionBus) BrowserOpenURL(url string)   {}

func (b *sessionBus) ClipboardGetText() (string, error) {
	return "", fmt.Errorf("网页端请使用浏览器剪贴板（Ctrl+V）")
}
func (b *sessionBus) ClipboardSetText(text string) error {
	return fmt.Errorf("网页端请使用浏览器剪贴板（Ctrl+C）")
}
func (b *sessionBus) SaveFileDialog(opts wailsrt.DialogOptions) (string, error) {
	return "", fmt.Errorf("网页端不支持文件对话框：导出内容将通过浏览器下载")
}
func (b *sessionBus) OpenFileDialog(opts wailsrt.DialogOptions) (string, error) {
	return "", fmt.Errorf("网页端不支持文件对话框：请使用「导入」界面选择内容粘贴")
}
func (b *sessionBus) OpenDirectoryDialog(opts wailsrt.DialogOptions) (string, error) {
	return "", fmt.Errorf("网页端不支持目录对话框")
}

// 编译期检查。
var _ bus.Bus = (*sessionBus)(nil)

// session 单用户会话：一个 appcore 实例 + 互斥锁。
type session struct {
	core *appcore.App
	mu   sync.Mutex // 串行化同用户的 RPC 调用（appcore 内部 dataMu 只保护数据读写，
	// RunAgent 等长流程需要会话级互斥防止交叉）
	lastUsed time.Time
}

// sessionManager 用户会话缓存：懒创建 + 空闲淘汰。
type sessionManager struct {
	s    *Server
	mu   sync.Mutex
	sess map[string]*session
	ttl  time.Duration
}

func newSessionManager(s *Server, ttl time.Duration) *sessionManager {
	m := &sessionManager{s: s, sess: map[string]*session{}, ttl: ttl}
	go m.reaper()
	return m
}

// get 取会话（不存在则懒创建）。
func (m *sessionManager) get(userID, username string) (*session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sess[userID]; ok {
		s.lastUsed = time.Now()
		return s, nil
	}
	us := store.NewUserStore(m.s.db, userID, m.s.cfg.DataDir)
	core, err := appcore.New(us, &sessionBus{s: m.s, userID: userID}, appcore.HostInfo{Version: m.s.version})
	if err != nil {
		return nil, err
	}
	sess := &session{core: core, lastUsed: time.Now()}
	m.sess[userID] = sess
	return sess, nil
}

// withLock 在会话互斥下执行 fn（RPC 入口统一走这里）。
func (m *sessionManager) withLock(userID, username string, fn func(core *appcore.App) (interface{}, error)) (interface{}, error) {
	sess, err := m.get(userID, username)
	if err != nil {
		return nil, err
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.lastUsed = time.Now()
	return fn(sess.core)
}

// reaper 周期淘汰空闲会话。
func (m *sessionManager) reaper() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for range t.C {
		m.mu.Lock()
		for uid, s := range m.sess {
			if time.Since(s.lastUsed) > m.ttl {
				delete(m.sess, uid)
			}
		}
		m.mu.Unlock()
	}
}

// Count 在线会话数（诊断）。
func (m *sessionManager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sess)
}

var _ = model.AppData{} // 占位
