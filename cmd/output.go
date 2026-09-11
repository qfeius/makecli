/**
 * [INPUT]: 依赖 bytes、encoding/json、fmt、os；依赖 internal/notifier 的 Pending
 * [OUTPUT]: 对外提供 list 命令通用的输出格式校验和 JSON 编码辅助函数；包内 attachNotice 把 _notice 追加到顶层 JSON 对象末尾
 * [POS]: cmd 模块的输出层辅助，所有 --output json 的唯一 stdout 出口；有待提示更新时在顶层对象末尾追加 _notice.update（对齐 lark-cli：agent 在非 TTY 下也能收到升级提示，stderr 文本提示只给 TTY 上的人看）
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/qfeius/makecli/internal/notifier"
)

const (
	outputTable = "table"
	outputJSON  = "json"
)

func validateOutputFormat(output string) error {
	switch output {
	case outputTable, outputJSON:
		return nil
	default:
		return fmt.Errorf("unsupported output format %q, valid options: %s, %s", output, outputTable, outputJSON)
	}
}

// writeJSON 把 v 以 2 空格缩进写到 stdout；有待提示更新时把 _notice 挂到顶层对象上。
func writeJSON(v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if u := notifier.Pending(); u != nil {
		notice, err := json.Marshal(map[string]any{"update": u})
		if err != nil {
			return err
		}
		body = attachNotice(body, notice)
	}
	var out bytes.Buffer
	if err := json.Indent(&out, body, "", "  "); err != nil {
		return err
	}
	out.WriteByte('\n')
	_, err = os.Stdout.Write(out.Bytes())
	return err
}

// attachNotice 把 "_notice": notice 追加为顶层 JSON 对象（紧凑编码）的最后一个成员。
// 不解析不重排，调用方的字段顺序原封不动；顶层不是对象（如数组、null）无处可挂，原样返回。
func attachNotice(obj, notice []byte) []byte {
	if len(obj) < 2 || obj[0] != '{' {
		return obj
	}
	var b bytes.Buffer
	b.Write(obj[:len(obj)-1]) // 去掉收尾 }
	if len(obj) > 2 {         // 空对象 {} 不补逗号
		b.WriteByte(',')
	}
	b.WriteString(`"_notice":`)
	b.Write(notice)
	b.WriteByte('}')
	return b.Bytes()
}
