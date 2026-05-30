/**
 * [INPUT]: 依赖 testing；包内 notifierEnabled（白盒）
 * [OUTPUT]: 单元测试，无导出
 * [POS]: internal/notifier 判定层测试，验证 notifierEnabled 对环境变量值做 TrimSpace
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package notifier

import "testing"

// TestNotifierEnabledTrimsWhitespace 验证带首尾空白的 env 值仍被正确解析，
// 不会因 ParseBool 不 trim 而静默下沉到默认值。不改变 fall-through 语义：
// 纯空白视为未设置，非法值仍下沉。
func TestNotifierEnabledTrimsWhitespace(t *testing.T) {
	tests := []struct {
		name   string
		envVal string
		cfgVal *bool
		want   bool
	}{
		{"leading/trailing space false", "  false  ", nil, false},
		{"tab-wrapped true", "\ttrue\t", nil, true},
		{"whitespace-only falls through to default", "   ", nil, true},
		{"clean false still works", "false", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := notifierEnabled(tt.envVal, tt.cfgVal); got != tt.want {
				t.Errorf("notifierEnabled(%q, %v) = %v, want %v", tt.envVal, tt.cfgVal, got, tt.want)
			}
		})
	}
}
