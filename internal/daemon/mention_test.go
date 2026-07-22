/**
 * [INPUT]: 依赖 mention.go 的 parseMentionBlocks
 * [OUTPUT]: 对外提供出站 mention 解析的回归——@Name 切分、邮箱不误伤、无 mention 原样单块
 * [POS]: internal/daemon 的互@解析测试面
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package daemon

import (
	"reflect"
	"testing"
)

func TestParseMentionBlocks(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []Block
	}{
		{
			name: "无 mention 原样单块",
			text: "普通回复,没有提到任何人",
			want: []Block{{Kind: "text", Text: "普通回复,没有提到任何人"}},
		},
		{
			name: "中英混排切分",
			text: "结论已出,@Make_Agent_2 请复核一下",
			want: []Block{
				{Kind: "text", Text: "结论已出,"},
				{Kind: "mention", Text: "Make_Agent_2", Target: &MentionTarget{Kind: "agent", ID: "Make_Agent_2"}},
				{Kind: "text", Text: " 请复核一下"},
			},
		},
		{
			name: "行首与相邻多 mention",
			text: "@Make_Agent_2 @Make_Agent_3 分头验证",
			want: []Block{
				{Kind: "mention", Text: "Make_Agent_2", Target: &MentionTarget{Kind: "agent", ID: "Make_Agent_2"}},
				{Kind: "text", Text: " "},
				{Kind: "mention", Text: "Make_Agent_3", Target: &MentionTarget{Kind: "agent", ID: "Make_Agent_3"}},
				{Kind: "text", Text: " 分头验证"},
			},
		},
		{
			name: "邮箱不误伤",
			text: "联系 admin@example.com 获取权限",
			want: []Block{{Kind: "text", Text: "联系 admin@example.com 获取权限"}},
		},
		{
			name: "标点收尾的名字截断",
			text: "(@Make_Agent_2,收到请回复)",
			want: []Block{
				{Kind: "text", Text: "("},
				{Kind: "mention", Text: "Make_Agent_2", Target: &MentionTarget{Kind: "agent", ID: "Make_Agent_2"}},
				{Kind: "text", Text: ",收到请回复)"},
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := parseMentionBlocks(testCase.text)
			if !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("parseMentionBlocks(%q) =\n %+v\nwant\n %+v", testCase.text, got, testCase.want)
			}
		})
	}
}
