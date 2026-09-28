package vault

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"srvctl/internal/model"
)

// 文件布局： magic(8) | salt(32) | nonce(12) | ciphertext | tag(16)
const (
	magic   = "SRVCTL1\x00"
	version = 1
)

var (
	// ErrLocked 表示 vault 尚未解锁。
	ErrLocked = errors.New("vault 已锁定")
	// ErrChanged 表示文件被其他进程/机器改写，且无法用当前密钥解开
	// （通常意味着在另一台机器上改了主密码）。
	ErrChanged = errors.New("vault 已被其他程序或机器修改，且当前密钥无法解密 —— 主密码可能已变更，请重新登录")
)

type document struct {
	Version int            `json:"version"`
	Servers []model.Server `json:"servers"`
}

// Store 是一个加密的服务器库。
//
// 所有公开方法都是并发安全的。内部 *Locked 方法假定调用方已持有 mu。
type Store struct {
	path string

	mu       sync.Mutex
	key      []byte
	salt     []byte
	servers  []model.Server
	lastMod  time.Time
	unlocked bool
}

// Open 构造一个指向 path 的 Store（此时并不读取文件）。
func Open(path string) *Store { return &Store{path: path} }

// Path 返回 vault 文件路径。
func (s *Store) Path() string { return s.path }

// Revision 返回一个代表 vault 当前内容的标记（文件 mtime + 大小）。
//
// 用途：界面靠它判断"有没有别的进程改过 vault" —— 比如 AI 通过 CLI
// 加了一台服务器、或者另一台机器同步过来。变了就该重新拉一次列表。
//
// 不需要加锁：只 stat 文件，不读写内存状态。
func (s *Store) Revision() string {
	fi, err := os.Stat(s.path)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d:%d", fi.ModTime().UnixNano(), fi.Size())
}

// Exists 报告 vault 文件是否存在且非空。
func (s *Store) Exists() bool {
	fi, err := os.Stat(s.path)
	return err == nil && fi.Size() > 0
}

// Unlocked 报告当前是否已解锁。
func (s *Store) Unlocked() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.unlocked
}

// Init 创建一个全新的 vault。若文件已存在则报错。
func (s *Store) Init(password string) error {
	if password == "" {
		return fmt.Errorf("主密码不能为空")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Exists() {
		return fmt.Errorf("vault 已存在: %s", s.path)
	}
	salt, err := newSalt()
	if err != nil {
		return err
	}
	s.salt = salt
	s.key = deriveKey(password, salt)
	s.servers = nil
	s.unlocked = true
	return s.persistLocked()
}

// Unlock 用主密码解锁。
func (s *Store) Unlock(password string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.unlockLocked(password)
}

// Lock 清除内存中的密钥与数据。
func (s *Store) Lock() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lockLocked()
}

// ChangePassword 改主密码（会用新盐重新加密整库）。
func (s *Store) ChangePassword(oldPassword, newPassword string) error {
	if newPassword == "" {
		return fmt.Errorf("新主密码不能为空")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.unlockLocked(oldPassword); err != nil {
		return err
	}
	salt, err := newSalt()
	if err != nil {
		return err
	}
	s.salt = salt
	s.key = deriveKey(newPassword, salt)
	return s.persistLocked()
}

// List 返回全部记录的副本。
//
// 注意返回的切片**永远不是 nil**。空 vault 时如果返回 nil，JSON 会序列化成
// null 而不是 []，前端拿到的就不是数组 —— 一个空库就能让界面崩掉。
func (s *Store) List() ([]model.Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireUnlockedLocked(); err != nil {
		return nil, err
	}
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	out := make([]model.Server, len(s.servers))
	copy(out, s.servers)
	return out, nil
}

// Get 按名称取一条记录。
func (s *Store) Get(name string) (model.Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireUnlockedLocked(); err != nil {
		return model.Server{}, err
	}
	if err := s.refreshLocked(); err != nil {
		return model.Server{}, err
	}
	for _, v := range s.servers {
		if v.Name == name {
			return v, nil
		}
	}
	return model.Server{}, fmt.Errorf("未找到服务器: %s", name)
}

// Upsert 按名称新增或整体替换一条记录。
func (s *Store) Upsert(srv model.Server) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireUnlockedLocked(); err != nil {
		return err
	}
	if err := s.refreshLocked(); err != nil {
		return err
	}
	srv.Normalize()
	if err := srv.Validate(); err != nil {
		return err
	}
	for i := range s.servers {
		if s.servers[i].Name == srv.Name {
			s.servers[i] = srv
			return s.persistLocked()
		}
	}
	s.servers = append(s.servers, srv)
	return s.persistLocked()
}

// Delete 按名称删除一条记录。
func (s *Store) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireUnlockedLocked(); err != nil {
		return err
	}
	if err := s.refreshLocked(); err != nil {
		return err
	}
	out := s.servers[:0]
	found := false
	for _, v := range s.servers {
		if v.Name == name {
			found = true
			continue
		}
		out = append(out, v)
	}
	if !found {
		return fmt.Errorf("未找到服务器: %s", name)
	}
	s.servers = out
	return s.persistLocked()
}

// Count 返回记录条数。
func (s *Store) Count() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireUnlockedLocked(); err != nil {
		return 0, err
	}
	return len(s.servers), nil
}

// ---------- 内部 ----------

func (s *Store) requireUnlockedLocked() error {
	if !s.unlocked || s.key == nil {
		return ErrLocked
	}
	return nil
}

func (s *Store) lockLocked() {
	for i := range s.key {
		s.key[i] = 0 // 真正清零，不只是丢引用
	}
	s.key = nil
	s.servers = nil
	s.unlocked = false
}

func (s *Store) unlockLocked(password string) error {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	if len(raw) < len(magic)+saltLen || string(raw[:len(magic)]) != magic {
		return fmt.Errorf("%s 不是有效的 srvctl vault 文件", s.path)
	}
	s.salt = append([]byte(nil), raw[len(magic):len(magic)+saltLen]...)
	key := deriveKey(password, s.salt)

	plain, err := open(raw[len(magic)+saltLen:], key)
	if err != nil {
		return ErrBadPassword
	}
	var doc document
	if err := json.Unmarshal(plain, &doc); err != nil {
		return ErrBadPassword
	}
	s.key = key
	s.servers = doc.Servers
	s.unlocked = true
	s.stampModTimeLocked()
	return nil
}

// refreshLocked 处理"vault 文件被外部改写"的情况：云盘同步、另一台机器写入、
// 或另一个进程。Conduit 用同样的思路处理 iCloud Drive 替换文件
// （electron/services/vault/vault.ts:286 reloadFromDisk）。
//
// 若文件比我们上次读/写时更新，就用内存中已有的密钥重新解密并替换内存状态。
func (s *Store) refreshLocked() error {
	fi, err := os.Stat(s.path)
	if err != nil {
		return nil // 文件不在了 —— 保持现状，下次 persist 会重建
	}
	if !fi.ModTime().After(s.lastMod) {
		return nil
	}
	raw, err := os.ReadFile(s.path)
	if err != nil || len(raw) < len(magic)+saltLen || string(raw[:len(magic)]) != magic {
		return nil
	}
	plain, err := open(raw[len(magic)+saltLen:], s.key)
	if err != nil {
		return ErrChanged
	}
	var doc document
	if err := json.Unmarshal(plain, &doc); err != nil {
		return ErrChanged
	}
	s.servers = doc.Servers
	s.lastMod = fi.ModTime()
	return nil
}

func (s *Store) persistLocked() error {
	if err := s.requireUnlockedLocked(); err != nil {
		return err
	}
	plain, err := json.MarshalIndent(document{Version: version, Servers: s.servers}, "", "  ")
	if err != nil {
		return err
	}
	blob, err := seal(plain, s.key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}

	out := make([]byte, 0, len(magic)+saltLen+len(blob))
	out = append(out, magic...)
	out = append(out, s.salt...)
	out = append(out, blob...)

	// 先写临时文件再原子 rename —— 中途崩溃不会损坏 vault，
	// 云盘同步冲突时最坏也只是多出一个"冲突副本"。
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return err
	}
	if err := renameWithRetry(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("写入 vault 失败: %w", err)
	}
	s.stampModTimeLocked()
	return nil
}

// renameWithRetry 在 Windows 上重试 rename。
//
// Windows 不允许替换一个正被其他进程打开的文件。杀毒软件扫描、Windows 搜索
// 索引、OneDrive 同步都可能在我们写到一半时短暂持有 vault 文件，导致
// os.Rename 报 "Access is denied" —— 而这是在"保存"这个最容易被注意到的
// 操作上，表现为保存失败。重试几次基本都能成功。
func renameWithRetry(oldPath, newPath string) error {
	var err error
	delay := 20 * time.Millisecond
	for attempt := 0; attempt < 6; attempt++ {
		if err = os.Rename(oldPath, newPath); err == nil {
			return nil
		}
		time.Sleep(delay)
		delay *= 2
	}
	return err
}

func (s *Store) stampModTimeLocked() {
	if fi, err := os.Stat(s.path); err == nil {
		s.lastMod = fi.ModTime()
	}
}
