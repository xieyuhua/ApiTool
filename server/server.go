package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// User 账号（密码用 sha256(salt+password) 存储）
type User struct {
	Username string `json:"username"`
	Salt     string `json:"salt"`
	Hash     string `json:"hash"`
	Token    string `json:"token"`
}

// CloudProject 云端项目（data 为项目完整 JSON）
type CloudProject struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Owner     string          `json:"owner"`
	UpdatedAt string          `json:"updatedAt"`
	Data      json.RawMessage `json:"data"`
}

type Store struct {
	dir   string
	mu    sync.Mutex
	users map[string]*User
}

func NewStore(dir string) *Store {
	_ = os.MkdirAll(dir, 0o755)
	_ = os.MkdirAll(filepath.Join(dir, "projects"), 0o755)
	s := &Store{dir: dir, users: map[string]*User{}}
	s.loadUsers()
	return s
}

func (s *Store) usersFile() string { return filepath.Join(s.dir, "users.json") }

func (s *Store) loadUsers() {
	b, err := os.ReadFile(s.usersFile())
	if err == nil {
		_ = json.Unmarshal(b, &s.users)
	}
}

func (s *Store) saveUsers() {
	b, _ := json.MarshalIndent(s.users, "", "  ")
	_ = os.WriteFile(s.usersFile(), b, 0o644)
}

func (s *Store) projFile(user, id string) string {
	return filepath.Join(s.dir, "projects", user, id+".json")
}

func hashPW(pw, salt string) string {
	h := sha256.Sum256([]byte(pw + ":" + salt))
	return hex.EncodeToString(h[:])
}

func newToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Store) auth(r *http.Request) (*User, bool) {
	token := ""
	if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		token = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	if token == "" {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.users {
		if u.Token == token {
			return u, true
		}
	}
	return nil, false
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// Handler 返回 HTTP 路由（含 CORS，便于 webview / 浏览器跨域访问）
func (s *Store) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/register", s.handleRegister)
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/projects", s.handleProjectsListCreate)
	mux.HandleFunc("/api/projects/", s.handleProjectByID)
	return cors(mux)
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Store) handleRegister(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Username == "" || body.Password == "" {
		writeJSON(w, 400, map[string]string{"error": "用户名和密码不能为空"})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[body.Username]; ok {
		writeJSON(w, 409, map[string]string{"error": "用户名已存在"})
		return
	}
	salt := newToken()
	u := &User{Username: body.Username, Salt: salt, Hash: hashPW(body.Password, salt), Token: newToken()}
	s.users[body.Username] = u
	s.saveUsers()
	writeJSON(w, 200, map[string]string{"token": u.Token})
}

func (s *Store) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "请求格式错误"})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[body.Username]
	if !ok || u.Hash != hashPW(body.Password, u.Salt) {
		writeJSON(w, 401, map[string]string{"error": "用户名或密码错误"})
		return
	}
	writeJSON(w, 200, map[string]string{"token": u.Token})
}

func (s *Store) handleProjectsListCreate(w http.ResponseWriter, r *http.Request) {
	u, ok := s.auth(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "未授权"})
		return
	}
	if r.Method == http.MethodGet {
		files, _ := filepath.Glob(filepath.Join(s.dir, "projects", u.Username, "*.json"))
		var list []map[string]string
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			var p CloudProject
			if json.Unmarshal(b, &p) != nil {
				continue
			}
			list = append(list, map[string]string{"id": p.ID, "name": p.Name, "updatedAt": p.UpdatedAt})
		}
		writeJSON(w, 200, list)
		return
	}
	if r.Method == http.MethodPost {
		var body struct {
			Name string          `json:"name"`
			Data json.RawMessage `json:"data"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Name == "" {
			body.Name = "未命名项目"
		}
		id := uuid.NewString()
		now := time.Now().Format(time.RFC3339)
		p := CloudProject{ID: id, Name: body.Name, Owner: u.Username, UpdatedAt: now, Data: body.Data}
		if err := s.saveProject(u.Username, &p); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"id": id, "updatedAt": now})
		return
	}
	writeJSON(w, 405, map[string]string{"error": "method not allowed"})
}

func (s *Store) handleProjectByID(w http.ResponseWriter, r *http.Request) {
	u, ok := s.auth(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "未授权"})
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/projects/")
	id = strings.Trim(id, "/")
	if id == "" {
		writeJSON(w, 400, map[string]string{"error": "缺少项目 ID"})
		return
	}
	if r.Method == http.MethodGet {
		p, err := s.loadProject(u.Username, id)
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": "项目不存在"})
			return
		}
		writeJSON(w, 200, p)
		return
	}
	if r.Method == http.MethodPut {
		var body struct {
			Name      string          `json:"name"`
			Data      json.RawMessage `json:"data"`
			UpdatedAt string          `json:"updatedAt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "请求格式错误"})
			return
		}
		existing, err := s.loadProject(u.Username, id)
		if err == nil && existing.UpdatedAt > body.UpdatedAt {
			writeJSON(w, 409, map[string]string{"error": "云端已有更新版本，请先拉取最新版本"})
			return
		}
		now := time.Now().Format(time.RFC3339)
		p := CloudProject{ID: id, Name: body.Name, Owner: u.Username, UpdatedAt: now, Data: body.Data}
		if err := s.saveProject(u.Username, &p); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"updatedAt": now})
		return
	}
	writeJSON(w, 405, map[string]string{"error": "method not allowed"})
}

func (s *Store) saveProject(user string, p *CloudProject) error {
	dir := filepath.Join(s.dir, "projects", user)
	_ = os.MkdirAll(dir, 0o755)
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.projFile(user, p.ID), b, 0o644)
}

func (s *Store) loadProject(user, id string) (*CloudProject, error) {
	b, err := os.ReadFile(s.projFile(user, id))
	if err != nil {
		return nil, err
	}
	var p CloudProject
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	return &p, nil
}
