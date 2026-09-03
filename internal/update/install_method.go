/**
 * [INPUT]: 依赖 os、os/exec、path/filepath、slices、strings
 * [OUTPUT]: 包内提供 detectInstallMethod / installMethodFromPath / packageManagerCommand / applyViaPackageManager；InstallMethod 枚举
 * [POS]: internal/update 的安装方式感知层——npm/pnpm 装的二进制位于 node_modules 下，原地替换会让包管理器的版本记录失真，
 *        Apply 在此分流：交还给拥有它的包管理器（npm install -g / pnpm add -g 指定版本），只有裸二进制安装才走下载-校验-替换
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package update

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// InstallMethod 标识当前运行的二进制由谁安装、谁负责升级。
type InstallMethod int

const (
	InstallBinary InstallMethod = iota // 裸二进制（Homebrew、手动下载）：自行替换
	InstallNpm                         // npm 全局安装：npm install -g
	InstallPnpm                        // pnpm 全局安装：pnpm add -g
)

// NpmPackage 是 npm 主包名，平台子包由它的 optionalDependencies 钉住。
const NpmPackage = "@qfeius/makecli"

// runCommand 是执行包管理器的 seam，测试替换以避免真实 npm 调用。
var runCommand = func(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// detectInstallMethod 解析当前 exe 真实路径判定安装方式；任何解析失败都退回裸二进制路径。
func detectInstallMethod() InstallMethod {
	exe, err := os.Executable()
	if err != nil {
		return InstallBinary
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return installMethodFromPath(exe)
}

// installMethodFromPath 按路径段分类：含 node_modules 即包管理器安装；
// pnpm 两种布局（虚拟仓 ".pnpm" 段、全局 CAS 仓 "pnpm/store"）单独识别，其余归 npm。
// 分隔符统一为 "/"，Windows 路径同样适用。
func installMethodFromPath(p string) InstallMethod {
	parts := strings.Split(strings.ReplaceAll(p, `\`, "/"), "/")
	if !slices.Contains(parts, "node_modules") {
		return InstallBinary
	}
	for i, part := range parts {
		if part == ".pnpm" || (part == "pnpm" && i+1 < len(parts) && parts[i+1] == "store") {
			return InstallPnpm
		}
	}
	return InstallNpm
}

// packageManagerCommand 拼出把 makecli 升到 version 的包管理器命令行（version 不带 v 前缀）。
func packageManagerCommand(m InstallMethod, version string) []string {
	spec := NpmPackage + "@" + version
	if m == InstallPnpm {
		return []string{"pnpm", "add", "-g", spec}
	}
	return []string{"npm", "install", "-g", spec}
}

// applyViaPackageManager 把升级委托给拥有该安装的包管理器，输出直通终端。
func applyViaPackageManager(m InstallMethod, version string) error {
	argv := packageManagerCommand(m, version)
	if _, err := exec.LookPath(argv[0]); err != nil {
		return fmt.Errorf("makecli was installed via %s but it is not on PATH; run: %s", argv[0], strings.Join(argv, " "))
	}
	if err := runCommand(argv[0], argv[1:]...); err != nil {
		return fmt.Errorf("%s failed: %w", strings.Join(argv, " "), err)
	}
	return nil
}
