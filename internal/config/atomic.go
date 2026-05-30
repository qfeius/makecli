/**
 * [INPUT]: 依赖 io、os、path/filepath
 * [OUTPUT]: 对外提供（包内）atomicWrite——同目录临时文件 + rename 原子替换
 * [POS]: internal/config 的落盘原语，被 credentials.Save / config.SaveConfig 复用，消除 O_TRUNC 写入中途崩溃的损坏窗口
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package config

import (
	"io"
	"os"
	"path/filepath"
)

// atomicWrite 把 render 产出的内容原子写入 path：先写同目录临时文件（保证同一文件系统
// 上的 rename 是原子的），成功后 rename 覆盖目标。render 出错或写入失败则清理临时文件、
// 不触碰目标——杜绝 O_TRUNC 直写在崩溃/并发时留下半截损坏文件的窗口。
func atomicWrite(path string, perm os.FileMode, render func(w io.Writer) error) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// 失败路径统一清理；成功 rename 后 tmpName 已不存在，Remove 无害。
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
	return os.Rename(tmpName, path)
}
