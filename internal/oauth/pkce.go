/**
 * [INPUT]: 依赖 crypto/rand、crypto/sha256、encoding/base64、fmt、io
 * [OUTPUT]: 对外提供 NewCodeVerifier / NewState / S256Challenge
 * [POS]: internal/oauth 的 PKCE 原语，被 login 流程生成 code_verifier / state / code_challenge
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

// NewCodeVerifier 生成 PKCE code_verifier（32 字节随机 → base64 raw-url）。
// reader 为 nil 时用 crypto/rand，测试可注入确定性 reader。
func NewCodeVerifier(reader io.Reader) (string, error) {
	if reader == nil {
		reader = rand.Reader
	}
	buf := make([]byte, 32)
	if _, err := io.ReadFull(reader, buf); err != nil {
		return "", fmt.Errorf("read random verifier bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// NewState 生成 OAuth state，与 code_verifier 同构（32 字节随机）。
func NewState(reader io.Reader) (string, error) {
	return NewCodeVerifier(reader)
}

// S256Challenge 计算 code_challenge = base64rawurl(sha256(verifier))。
func S256Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
