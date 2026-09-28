package web

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestServer() *Server {
	return &Server{
		token:   "test-token",
		log:     log.New(io.Discard, "", 0),
		logPath: `C:\test\srvctl.log`,
	}
}

// panic 必须被兜住并转成 500，而不是让 net/http 静默关闭连接。
// 静默关闭在浏览器端就是一句 "Failed to fetch"，用户拿不到任何线索。
func TestRecoverPanicTurnsPanicInto500(t *testing.T) {
	s := newTestServer()
	h := s.recoverPanic(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})

	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/api/x", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("状态码应为 500，得到 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "boom") {
		t.Errorf("响应体应包含 panic 原因，得到 %q", body)
	}
	if !strings.Contains(body, "srvctl.log") {
		t.Errorf("响应体应指出日志位置，得到 %q", body)
	}
}

// 响应头已经写出之后再 panic，不能再补 500 —— 那会破坏已经发出的响应。
func TestRecoverPanicAfterWriteKeepsOriginalStatus(t *testing.T) {
	s := newTestServer()
	h := s.recoverPanic(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("partial"))
		panic("too late")
	})

	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/api/x", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("已写出响应头后应保持原状态码 200，得到 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "partial") {
		t.Errorf("原有响应体应保留，得到 %q", rec.Body.String())
	}
}

// 没有 panic 时行为必须完全不变。
func TestRecoverPanicPassesThrough(t *testing.T) {
	s := newTestServer()
	h := s.recoverPanic(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/api/x", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码应为 200，得到 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Errorf("响应体不正确: %q", rec.Body.String())
	}
}

// token 不对必须拿到 403 + JSON 错误体，而不是断连。
// 前端靠这个区分"令牌失效"和"服务没响应"，给出不同的提示。
func TestGuardRejectsBadTokenWithJSON(t *testing.T) {
	s := newTestServer()
	h := s.recoverPanic(s.guard(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("handler 不应该被调用")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	req.Header.Set("X-Srvctl-Token", "wrong")
	h(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("状态码应为 403，得到 %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type 应为 JSON，得到 %q", ct)
	}
}

func TestGuardAcceptsGoodToken(t *testing.T) {
	s := newTestServer()
	called := false
	h := s.recoverPanic(s.guard(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	req.Header.Set("X-Srvctl-Token", "test-token")
	h(rec, req)

	if !called {
		t.Fatal("handler 应该被调用")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("状态码应为 200，得到 %d", rec.Code)
	}
}
