package vault

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"srvctl/internal/model"
)

func tempStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vault.enc")
	st := Open(path)
	if err := st.Init("test-password"); err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	return st
}

// 回归测试：空 vault 的 List() 不能返回 nil。
//
// 返回 nil 时 JSON 会序列化成 null，前端拿到 null 后 [...null] 直接抛
// "is not iterable" —— 一个全新空库就能让界面崩掉，而且只在第一次使用时
// 出现，最容易漏测。
func TestListOnEmptyVaultIsNonNil(t *testing.T) {
	st := tempStore(t)

	got, err := st.List()
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if got == nil {
		t.Fatal("空 vault 的 List() 返回了 nil，JSON 会变成 null 而不是 []")
	}
	if len(got) != 0 {
		t.Fatalf("应为空切片，得到 %d 条", len(got))
	}
}

// 直接钉住 JSON 形状 —— 这是前端真正依赖的契约。
func TestEmptyVaultMarshalsToEmptyArray(t *testing.T) {
	st := tempStore(t)

	servers, err := st.List()
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	blob, err := json.Marshal(map[string]any{"servers": servers})
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	if !strings.Contains(string(blob), `"servers":[]`) {
		t.Errorf("空库应序列化为 \"servers\":[]，实际得到 %s", blob)
	}
	if strings.Contains(string(blob), `"servers":null`) {
		t.Errorf("空库被序列化成了 null: %s", blob)
	}
}

// 加上一条记录后同样要是数组。
func TestListAfterInsertIsArray(t *testing.T) {
	st := tempStore(t)

	srv := model.Server{
		Name: "a", Host: "10.0.0.1", Platform: model.PlatformLinux,
		Username: "root", AuthMode: model.AuthPassword, Password: "p",
	}
	if err := st.Upsert(srv); err != nil {
		t.Fatalf("Upsert 失败: %v", err)
	}

	got, err := st.List()
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if len(got) != 1 || got[0].Name != "a" {
		t.Fatalf("应有 1 条 a，得到 %+v", got)
	}
}

// List 返回的必须是副本 —— 调用方改了不能影响 vault 内部状态。
func TestListReturnsCopy(t *testing.T) {
	st := tempStore(t)
	if err := st.Upsert(model.Server{
		Name: "a", Host: "10.0.0.1", Platform: model.PlatformLinux,
		Username: "root", AuthMode: model.AuthPassword, Password: "p",
	}); err != nil {
		t.Fatalf("Upsert 失败: %v", err)
	}

	first, _ := st.List()
	first[0].Name = "被改掉了"

	second, _ := st.List()
	if second[0].Name != "a" {
		t.Errorf("List() 没返回副本，内部状态被调用方修改了")
	}
}

// 错误的密码必须被拒绝，且不留下已解锁状态。
func TestUnlockWrongPasswordLeavesLocked(t *testing.T) {
	st := tempStore(t)
	st.Lock()

	if err := st.Unlock("wrong"); err == nil {
		t.Fatal("错误密码不该解锁成功")
	}
	if st.Unlocked() {
		t.Error("失败后不应处于已解锁状态")
	}
}

// 改密码后旧密码失效、新密码可用，数据保留。
func TestChangePassword(t *testing.T) {
	st := tempStore(t)
	if err := st.Upsert(model.Server{
		Name: "a", Host: "10.0.0.1", Platform: model.PlatformLinux,
		Username: "root", AuthMode: model.AuthPassword, Password: "p",
	}); err != nil {
		t.Fatalf("Upsert 失败: %v", err)
	}
	if err := st.ChangePassword("test-password", "new-password"); err != nil {
		t.Fatalf("ChangePassword 失败: %v", err)
	}
	st.Lock()

	if err := st.Unlock("test-password"); err == nil {
		t.Error("旧密码不该还能解锁")
	}
	if err := st.Unlock("new-password"); err != nil {
		t.Fatalf("新密码解锁失败: %v", err)
	}
	got, _ := st.List()
	if len(got) != 1 || got[0].Name != "a" {
		t.Errorf("改密码后数据丢失: %+v", got)
	}
}
