/**
 * [INPUT]: 依赖 cmd 包内的 mask、validateJWT、validateConfigKey（包内白盒）
 * [OUTPUT]: 覆盖凭证遮掩、JWT 校验、config key 校验的单元测试
 * [POS]: cmd 模块 configure.go 的配套测试
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import "testing"

func TestMask(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"ab", "**"},
		{"abcd", "****"},         // 恰好 4 位 → 全遮掩
		{"abcde", "*bcde"},       // 5 位 → 1 星 + 末4位
		{"hello", "*ello"},
		{"12345678", "****5678"}, // 8 位 → 4 星 + 末4位
	}

	for _, tt := range tests {
		got := mask(tt.input)
		if got != tt.want {
			t.Errorf("mask(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestValidateJWT(t *testing.T) {
	// ---------------------------------- 合法 JWT ----------------------------------
	// header.payload.signature 每段均为合法 base64url
	validSeg := "eyJhbGciOiJIUzI1NiJ9" // {"alg":"HS256"}
	validJWT := validSeg + "." + validSeg + "." + validSeg

	if err := validateJWT(validJWT); err != nil {
		t.Errorf("valid JWT returned error: %v", err)
	}

	// ---------------------------------- 非法格式 ----------------------------------
	cases := []struct {
		name  string
		token string
	}{
		{"two segments", "only.two"},
		{"four segments", "a.b.c.d"},
		{"invalid base64url in first segment", "invalid!@#." + validSeg + "." + validSeg},
		{"empty string", ""},
	}

	for _, tt := range cases {
		if err := validateJWT(tt.token); err == nil {
			t.Errorf("validateJWT(%q) [%s]: expected error, got nil", tt.token, tt.name)
		}
	}
}

func TestValidConfigKeys(t *testing.T) {
	if err := validateConfigKey("server-url"); err != nil {
		t.Errorf("server-url should be valid: %v", err)
	}
	if err := validateConfigKey("X-Tenant-ID"); err != nil {
		t.Errorf("X-Tenant-ID should be valid: %v", err)
	}
	if err := validateConfigKey("X-Operator-ID"); err != nil {
		t.Errorf("X-Operator-ID should be valid: %v", err)
	}
	if err := validateConfigKey("bad-key"); err == nil {
		t.Error("bad-key should be invalid")
	}
}
