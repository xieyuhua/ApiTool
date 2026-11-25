package serverclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// fakeServer 模拟服务端：登录 + /rpc/LoadData/SaveData + 401 刷新。
func fakeServer(t *testing.T) (*httptest.Server, *atomic.Int64, *bool) {
	var saves atomic.Int64
	refreshedFlag := false
	mux := http.NewServeMux()
	validTok := "tok-1"

	mux.HandleFunc("/api/login", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Username, Password string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Username == "" || req.Password == "" {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"ok":false,"error":"用户名或密码错误"}`))
			return
		}
		_, _ = w.Write([]byte(`{"accessToken":"tok-1","refreshToken":"rt-1","user":{"id":"u1","username":"alice"},"projects":[{"id":"p1","name":"P1","role":"admin","version":3}]}`))
	})
	mux.HandleFunc("/api/token/refresh", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ RefreshToken string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.RefreshToken != "rt-1" {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":"无效"}`))
			return
		}
		refreshedFlag = true
		validTok = "tok-2"
		_, _ = w.Write([]byte(`{"accessToken":"tok-2","refreshToken":"rt-2","user":{"id":"u1","username":"alice"}}`))
	})
	mux.HandleFunc("/rpc/LoadData", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+validTok {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":"未登录"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"projects":[{"id":"p1","name":"服务端项目"}],"currentProjectId":"p1"}}`))
	})
	mux.HandleFunc("/rpc/SaveData", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+validTok {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":"未登录"}`))
			return
		}
		saves.Add(1)
		_, _ = w.Write([]byte(`{"ok":true,"result":null}`))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &saves, &refreshedFlag
}

func TestLoginAndRPC(t *testing.T) {
	srv, saves, _ := fakeServer(t)
	c := New(srv.URL)
	if err := c.Login("alice", "pass"); err != nil {
		t.Fatal(err)
	}
	if !c.LoggedIn() || c.UserID() != "u1" {
		t.Fatalf("登录态: user=%s", c.UserID())
	}
	// LoadData
	var data struct {
		Projects []struct{ ID string } `json:"projects"`
	}
	// 模拟 storeapi 的调用方式
	if err := c.CallRPC("LoadData", nil, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Projects) != 1 || data.Projects[0].ID != "p1" {
		t.Fatalf("LoadData: %+v", data)
	}
	// SaveData
	if err := c.CallRPC("SaveData", []json.RawMessage{json.RawMessage(`{}`)}, nil); err != nil {
		t.Fatal(err)
	}
	if saves.Load() != 1 {
		t.Fatalf("SaveData 次数: %d", saves.Load())
	}
}

func TestRefreshOn401(t *testing.T) {
	srv, _, refreshed := fakeServer(t)
	c := New(srv.URL)
	if err := c.Login("alice", "pass"); err != nil {
		t.Fatal(err)
	}
	// 模拟 access 过期：服务端只认 tok-1，而客户端 token 被外部改成旧值
	c.mu.Lock()
	c.accessToken = "expired"
	c.mu.Unlock()
	var data struct {
		Projects []struct{ ID string } `json:"projects"`
	}
	if err := c.CallRPC("LoadData", nil, &data); err != nil {
		t.Fatalf("401 应自动刷新重试: %v", err)
	}
	if len(data.Projects) != 1 {
		t.Fatal("重试后应成功")
	}
	if !*refreshed {
		t.Fatal("应触发过刷新")
	}
}

func TestCredentialsPersistence(t *testing.T) {
	srv, _, _ := fakeServer(t)
	path := filepath.Join(t.TempDir(), "server_session.json")
	c := New(srv.URL)
	if err := c.Login("alice", "pass"); err != nil {
		t.Fatal(err)
	}
	if err := c.SaveCredentials(path); err != nil {
		t.Fatal(err)
	}
	// 新客户端恢复
	c2 := New(srv.URL)
	if !c2.RestoreCredentials(path) {
		t.Fatal("凭据恢复失败")
	}
	if !c2.LoggedIn() || c2.UserID() != "u1" || c2.Username() != "alice" {
		t.Fatalf("恢复态: %s/%s", c2.UserID(), c2.Username())
	}
	// 清除
	c2.ClearCredentials(path)
	if c2.LoggedIn() {
		t.Fatal("清除后不应有登录态")
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("凭据文件应被删除")
	}
}
