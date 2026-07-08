/**
 * [INPUT]: 依赖 bytes、context、errors、slices、strings、testing
 * [OUTPUT]: 覆盖 skills remove 子命令的透传/报错/必填参数
 * [POS]: cmd/skills remove 子命令测试，removeSkillsFunc 打桩避免真实执行 npx
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// stubRemoveSkills 打桩 removeSkillsFunc，记录传入名字并返回给定错误。
func stubRemoveSkills(t *testing.T, err error) *[]string {
	t.Helper()
	var got []string
	orig := removeSkillsFunc
	removeSkillsFunc = func(ctx context.Context, names []string) error {
		got = names
		return err
	}
	t.Cleanup(func() { removeSkillsFunc = orig })
	return &got
}

func TestSkillsRemoveSuccess(t *testing.T) {
	gotNames := stubRemoveSkills(t, nil)

	cmd := newSkillsRemoveCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"makedsl", "makeui"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if !slices.Equal(*gotNames, []string{"makedsl", "makeui"}) {
		t.Fatalf("unexpected names: %v", *gotNames)
	}
	if !strings.Contains(buf.String(), "Removed: makedsl, makeui") {
		t.Fatalf("missing confirmation output:\n%s", buf.String())
	}
}

func TestSkillsRemoveError(t *testing.T) {
	stubRemoveSkills(t, errors.New("not installed Make platform skills: x"))

	cmd := newSkillsRemoveCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"x"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error to propagate")
	}
}

func TestSkillsRemoveRequiresArgs(t *testing.T) {
	stubRemoveSkills(t, nil)

	cmd := newSkillsRemoveCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{})

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when no names given")
	}
}
