/**
 * [INPUT]: 依赖 io、os、path/filepath；依赖 atomic_replace.go / atomic_replace_windows.go 的 ReplaceFile（平台分支）
 * [OUTPUT]: 对外提供（包内）atomicWrite——同目录临时文件 + ReplaceFile 原子替换
 * [POS]: internal/config 的落盘原语，被 credentials.Save / config.SaveConfig 复用，消除 O_TRUNC 写入中途崩溃的损坏窗口；
 *        覆盖既有目标的 rename 语义因平台而异（Windows 目标被占用时会失败），故替换动作收口到 build-tag 分发的 ReplaceFile
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package config

import (
	"io"
	"os"
	"path/filepath"
)

// atomicWrite 把 render 产出的内容原子写入 path：先写同目录临时文件（保证同一文件系统
// 上的替换是原子的），成功后经 ReplaceFile 覆盖目标（POSIX 直接 rename，Windows 用
// 单步覆盖原语退避重试，见 atomic_replace*.go）。render 出错或写入失败则清理临时文件、
// 不触碰目标——杜绝 O_TRUNC 直写在崩溃/并发时留下半截损坏文件的窗口。
func atomicWrite(path string, perm os.FileMode, render func(w io.Writer) error) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// 失败路径统一清理；成功替换后 tmpName 已不存在，Remove 无害。
	defer func() { _ = os.Remove(tmpName) }()

	if err := render(tmp); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return ReplaceFile(tmpName, path)
}
