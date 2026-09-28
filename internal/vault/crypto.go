// Package vault 实现加密的服务器库存。
//
// 密码学设计（与 Conduit 的 electron/services/vault/crypto.ts 保持一致）：
//   - 密钥派生：PBKDF2-SHA256，600,000 次迭代，32 字节输出
//   - 加密算法：AES-256-GCM
//   - 密文布局：nonce(12) || ciphertext || tag(16)
//
// 没有 SQLite。整个 vault 是一个加密的 JSON 文件 —— 对单人、几百条记录的
// 场景，SQLite 的 schema/迁移/WAL 都是纯粹的负担。
package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"

	"golang.org/x/crypto/pbkdf2"
)

const (
	pbkdf2Iters = 600_000
	keyLen      = 32 // AES-256
	saltLen     = 32
)

// ErrBadPassword 表示主密码错误，或文件被篡改（GCM 认证失败）。
var ErrBadPassword = errors.New("主密码错误")

// deriveKey 用 PBKDF2-SHA256 从主密码派生 32 字节密钥。
func deriveKey(password string, salt []byte) []byte {
	return pbkdf2.Key([]byte(password), salt, pbkdf2Iters, keyLen, sha256.New)
}

func newSalt() ([]byte, error) {
	s := make([]byte, saltLen)
	if _, err := rand.Read(s); err != nil {
		return nil, fmt.Errorf("生成随机盐失败: %w", err)
	}
	return s, nil
}

// seal 输出 nonce || ciphertext || tag。
//
// GCM.Seal 在 dst 非空时会先写入 dst 再追加密文，所以传 nonce 作 dst
// 就自动得到了"前置 nonce"的布局。
func seal(plain, key []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("生成 nonce 失败: %w", err)
	}
	return gcm.Seal(nonce, nonce, plain, nil), nil
}

// open 解封 seal 产生的密文。认证失败时返回错误。
func open(sealed, key []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(sealed) < ns {
		return nil, fmt.Errorf("密文长度不足")
	}
	return gcm.Open(nil, sealed[:ns], sealed[ns:], nil)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != keyLen {
		return nil, fmt.Errorf("密钥长度必须为 %d 字节", keyLen)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
