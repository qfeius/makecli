/**
 * [INPUT]: 依赖 testing、strings
 * [OUTPUT]: 提供 assertGeneratedAgentsContract 测试辅助函数，集中校验 makecli 生成的 AGENTS.md 关键合同
 * [POS]: cmd 测试共享断言，避免 app init / app create 重复锁定大量自然语言文案；只验证生成引导必须保留的本地预览、统一登录、Service 代理、运行时和发布前验证边界
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"strings"
	"testing"
)

func assertGeneratedAgentsContract(t *testing.T, content string) {
	t.Helper()

	mustContain := []string{
		"Vibe App Workflow",
		"<require_skills>",
		"<definition_of_done>",
		"<publish_prepare>",
		"apps/docs/PRD.md",
		"apps/docs/api.md",
		"makecli diff -f apps/dsl",
		"不要静默修改全局 skill 环境",
		"以本文件的硬约束为准",
		"页面优先",
		"make-app-auth",
		"unified login",
		"MAKE_APP_LOCAL_PREVIEW=true",
		"localPreview=true",
		"makecli login",
		"共用这条代码路径",
		"no-login",
		"unifiedLogin: false",
		`gatewayBaseUrl: "/api/make"`,
		"/api/make/auth/**",
		"/api/make/oauth/**",
		"make-app-service",
		"make-app-filter",
		"make-app-runtime",
		"3100 端口启动",
		"apps/ui/dist",
		"apps/service/dist/server.js",
		"pnpm run dev",
		"pnpm run build",
		"audit-auth-contract.mjs",
		"--mode service-fronted --published",
		"makecli app deploy",
	}
	for _, want := range mustContain {
		if !strings.Contains(content, want) {
			t.Errorf("AGENTS.md missing contract marker %q", want)
		}
	}

	mustNotContain := []string{
		"Stage Glossary",
		"<stage_glossary>",
		"</skill_routing>",
		"6000",
		"makecli app preflight",
		"make:predeploy",
		"MAKE_API_TOKEN",
	}
	for _, forbidden := range mustNotContain {
		if strings.Contains(content, forbidden) {
			t.Errorf("AGENTS.md should not contain obsolete marker %q", forbidden)
		}
	}
}
