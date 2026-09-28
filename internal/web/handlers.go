package web

import (
	"encoding/json"
	"net/http"
	"time"

	"srvctl/internal/config"
	"srvctl/internal/model"
	"srvctl/internal/netid"
	"srvctl/internal/reach"
	"srvctl/internal/sessionkey"
	"srvctl/internal/snippet"
	"srvctl/internal/vault"
)

// ---------- 响应辅助 ----------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func decodeBody(r *http.Request, v any) error {
	defer func() { _ = r.Body.Close() }()
	return json.NewDecoder(r.Body).Decode(v)
}

// ---------- 状态 ----------

// handlePing 供界面定期探活。
//
// 它同时验证了两件事：服务进程还活着，以及页面手里的 token 还有效
// （token 每次启动都会重新生成）。界面据此在断开时给出明确提示，
// 而不是让用户对着一句 "Failed to fetch" 猜。
func (s *Server) handlePing(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"unlocked": s.store.Unlocked(),
	})
}

type stateResponse struct {
	HasVault   bool                    `json:"has_vault"`
	Unlocked   bool                    `json:"unlocked"`
	Remembered bool                    `json:"remembered"`
	VaultPath  string                  `json:"vault_path"`
	Portable   bool                    `json:"portable"`
	SSID       string                  `json:"ssid"`
	Servers    []model.Server          `json:"servers"`
	Results    map[string]reach.Result `json:"results"`
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	resp := stateResponse{
		HasVault:   s.store.Exists(),
		Unlocked:   s.store.Unlocked(),
		Remembered: sessionkey.Exists(config.SessionKeyPath(s.portable)),
		VaultPath:  s.store.Path(),
		Portable:   s.portable,
		SSID:       s.getSSID(),
		Servers:    []model.Server{},
		Results:    s.getResults(),
	}
	if resp.Unlocked {
		servers, err := s.store.List()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		resp.Servers = servers
	}
	writeJSON(w, http.StatusOK, resp)
}

// ---------- 生命周期 ----------

func (s *Server) handleInit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
		Remember bool   `json:"remember"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体格式错误")
		return
	}
	if err := s.store.Init(body.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Remember {
		if err := sessionkey.Save(config.SessionKeyPath(s.portable), body.Password); err != nil {
			s.log.Printf("保存主密码失败（不影响本次使用）: %v", err)
		}
	}
	s.setResults(map[string]reach.Result{})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "vault_path": s.store.Path()})
}

func (s *Server) handleUnlock(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
		Remember bool   `json:"remember"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体格式错误")
		return
	}
	if err := s.store.Unlock(body.Password); err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if body.Remember {
		if err := sessionkey.Save(config.SessionKeyPath(s.portable), body.Password); err != nil {
			s.log.Printf("保存主密码失败（不影响本次使用）: %v", err)
		}
	}
	s.setSSID(netid.SSID())
	s.setResults(map[string]reach.Result{})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleLock(w http.ResponseWriter, r *http.Request) {
	s.store.Lock()
	s.setResults(map[string]reach.Result{})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleQuit(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	go func() {
		time.Sleep(200 * time.Millisecond) // 让响应先刷出去
		s.requestQuit()
	}()
}

// ---------- 探测 ----------

func (s *Server) handleTest(w http.ResponseWriter, r *http.Request) {
	if !s.store.Unlocked() {
		writeError(w, http.StatusLocked, vault.ErrLocked.Error())
		return
	}
	servers, err := s.store.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	ssid := netid.SSID()
	s.setSSID(ssid)

	results := reach.CheckAll(r.Context(), servers, ssid)
	indexed := reach.Index(results)
	s.setResults(indexed)

	writeJSON(w, http.StatusOK, map[string]any{
		"ssid":    ssid,
		"results": indexed,
	})
}

// ---------- 服务器记录 ----------

func (s *Server) handleServers(w http.ResponseWriter, r *http.Request) {
	if !s.store.Unlocked() {
		writeError(w, http.StatusLocked, vault.ErrLocked.Error())
		return
	}

	switch r.Method {
	case http.MethodPost, http.MethodPut:
		var srv model.Server
		if err := decodeBody(r, &srv); err != nil {
			writeError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
			return
		}
		srv.Normalize()
		if err := s.store.Upsert(srv); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": srv.Name})

	default:
		servers, err := s.store.List()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"servers": servers})
	}
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	if !s.store.Unlocked() {
		writeError(w, http.StatusLocked, vault.ErrLocked.Error())
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "缺少 name 参数")
		return
	}
	if err := s.store.Delete(name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------- 给 AI 的连接说明 ----------

func (s *Server) handleSnippet(w http.ResponseWriter, r *http.Request) {
	if !s.store.Unlocked() {
		writeError(w, http.StatusLocked, vault.ErrLocked.Error())
		return
	}
	servers, err := s.store.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	name := r.URL.Query().Get("name")
	ssid := s.getSSID()
	results := s.getResults()

	if name == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"text": snippet.ForAll(servers, results, ssid),
		})
		return
	}

	for _, srv := range servers {
		if srv.Name != name {
			continue
		}
		var ptr *reach.Result
		if v, ok := results[name]; ok {
			ptr = &v
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"text": snippet.For(srv, ptr, ssid),
		})
		return
	}
	writeError(w, http.StatusNotFound, "未找到服务器: "+name)
}
