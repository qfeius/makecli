/**
 * [INPUT]: 依赖 encoding/xml、errors、fmt、os、os/exec、path/filepath、regexp、sort、strconv、strings、text/template
 * [OUTPUT]: 对外提供 Label、Config、Status、ErrNotInstalled 与 Render / PlistPath / Install / Uninstall / Restart / Query
 * [POS]: internal/daemon 的 macOS 托管面：把前台 `makecli daemon` 固化成用户级 LaunchAgent，
 *        由 launchd 负责登录自启与崩溃拉起；launchctl 调用收口在 runLaunchctl 单一出口（测试可打桩）
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package launchd

import (
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"
)

// Label 是 LaunchAgent 的服务标识，同时决定 plist 文件名。
// 反向域名是 launchd 的硬约定；改这个常量等于换一个服务——旧 agent 必须先 stop 再改。
const Label = "cn.qfei.makecli.daemon"

// ErrNotInstalled 表示本机没有 makecli 的 LaunchAgent（plist 不存在）——
// 调用方据此给出「先 makecli daemon start」的可操作指引，而不是把 launchctl 的原始报错抛给用户。
var ErrNotInstalled = errors.New("launchd: LaunchAgent 未安装")

// Config 是渲染 plist 所需的全部事实。
// launchd 拉起的进程不继承用户 shell 环境（没有 .zshrc、没有 shell 里 export 的变量），
// 因此这里每一项都必须是「已解析的最终值」：参数不留 env 兜底，PATH 显式带上，
// 否则 daemon 会在 launchd 下找不到 claude / codex 二进制。
type Config struct {
	BinaryPath string            // makecli 绝对路径（调用方负责解符号链接）
	Args       []string          // 传给 makecli 的参数，不含 argv[0]
	Env        map[string]string // 注入 LaunchAgent 的环境变量（PATH 必带）
	WorkingDir string
	LogPath    string // stdout 与 stderr 合流到同一文件——daemon 的 slog 全写 stderr
}

// Status 是一次托管状态查询的结果。Installed（plist 在不在）与 Running（进程活没活）
// 是两件独立的事：stop 后两者皆假，崩溃重启间隙则 Installed 真而 Running 假。
type Status struct {
	Label          string
	Installed      bool // plist 存在 = 已托管
	Loaded         bool // launchctl 认得这个 service
	Running        bool // 有活着的 PID
	PID            int
	LastExitStatus int
	HasLastExit    bool
	PlistPath      string
	Command        []string // plist 里的 ProgramArguments（含二进制自身）
	LogPath        string
}

// runLaunchctl 是 launchctl 调用的唯一出口（单测打桩点）。
// 用 CombinedOutput：launchctl 的失败原因几乎都在输出里，退出码本身不带信息。
var runLaunchctl = func(args ...string) (string, error) {
	output, err := exec.Command("launchctl", args...).CombinedOutput()
	return string(output), err
}

// domainTarget 是当前登录用户的 GUI 域——用户级 LaunchAgent 的归属域。
func domainTarget() string { return fmt.Sprintf("gui/%d", os.Getuid()) }

// serviceTarget 是 domain 内的服务地址，bootout / kickstart 的操作对象。
func serviceTarget() string { return domainTarget() + "/" + Label }

// PlistPath 返回 LaunchAgent plist 的绝对路径（~/Library/LaunchAgents/<Label>.plist）。
func PlistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("无法获取 home 目录: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", Label+".plist"), nil
}

var plistTemplate = template.Must(template.New("plist").Funcs(template.FuncMap{
	"xml": escapeXML,
}).Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>{{xml .Label}}</string>
	<key>ProgramArguments</key>
	<array>
		<string>{{xml .BinaryPath}}</string>
{{- range .Args}}
		<string>{{xml .}}</string>
{{- end}}
	</array>
	<key>EnvironmentVariables</key>
	<dict>
{{- range .EnvKeys}}
		<key>{{xml .}}</key>
		<string>{{xml (index $.Env .)}}</string>
{{- end}}
	</dict>
	<key>WorkingDirectory</key>
	<string>{{xml .WorkingDir}}</string>
	<key>StandardOutPath</key>
	<string>{{xml .LogPath}}</string>
	<key>StandardErrorPath</key>
	<string>{{xml .LogPath}}</string>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
</dict>
</plist>
`))

// escapeXML 把值转义进 plist 文本。路径里的 & < > ' " 会直接破坏 XML，
// launchd 拒绝加载坏 plist 且报错晦涩，所以每个值都过一遍。
func escapeXML(value string) string {
	var buffer strings.Builder
	if err := xml.EscapeText(&buffer, []byte(value)); err != nil {
		return value
	}
	return buffer.String()
}

// Render 渲染 plist 文本。环境变量按键名排序输出，保证同样的 Config 渲染出同样的字节
// （否则每次 start 都产生"变了但没变"的 plist）。
func Render(config Config) (string, error) {
	envKeys := make([]string, 0, len(config.Env))
	for key := range config.Env {
		envKeys = append(envKeys, key)
	}
	sort.Strings(envKeys)

	data := struct {
		Label      string
		BinaryPath string
		Args       []string
		Env        map[string]string
		EnvKeys    []string
		WorkingDir string
		LogPath    string
	}{
		Label:      Label,
		BinaryPath: config.BinaryPath,
		Args:       config.Args,
		Env:        config.Env,
		EnvKeys:    envKeys,
		WorkingDir: config.WorkingDir,
		LogPath:    config.LogPath,
	}

	var buffer strings.Builder
	if err := plistTemplate.Execute(&buffer, data); err != nil {
		return "", fmt.Errorf("渲染 plist 失败: %w", err)
	}
	return buffer.String(), nil
}

// Install 写入 plist 并交给 launchd 拉起，返回 plist 路径。
// 覆盖式安装：先 bootout 旧实例（未加载则忽略）再 bootstrap，
// 这样改了参数重跑 start 立刻生效，而不是被"已加载"挡回去。
func Install(config Config) (string, error) {
	plistPath, err := PlistPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		return "", fmt.Errorf("创建 LaunchAgents 目录失败: %w", err)
	}
	if config.LogPath != "" {
		if err := os.MkdirAll(filepath.Dir(config.LogPath), 0o755); err != nil {
			return "", fmt.Errorf("创建日志目录失败: %w", err)
		}
	}
	document, err := Render(config)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(plistPath, []byte(document), 0o644); err != nil {
		return "", fmt.Errorf("写入 plist 失败: %w", err)
	}
	if err := bootstrap(plistPath); err != nil {
		return "", err
	}
	return plistPath, nil
}

// Uninstall 停服并移除 plist，返回此前是否处于托管状态。
// 删文件是 stop 语义的必要一半：只 bootout 不删，下次登录 launchd 扫描
// ~/Library/LaunchAgents 又会把它拉起来——"停了却自己回来"是最坏的意外。
func Uninstall() (bool, error) {
	plistPath, err := PlistPath()
	if err != nil {
		return false, err
	}
	_, statErr := os.Stat(plistPath)
	switch {
	case statErr == nil:
	case os.IsNotExist(statErr):
		bootout() // plist 已不在但服务可能还加载着（手工删过），照样清一次
		return false, nil
	default:
		return false, fmt.Errorf("读取 plist 失败: %w", statErr)
	}
	bootout()
	if err := os.Remove(plistPath); err != nil {
		return true, fmt.Errorf("删除 plist 失败: %w", err)
	}
	return true, nil
}

// Restart 用磁盘上现有的 plist 重新拉起——刻意不重新渲染，
// 因而 start 时敲定的参数（gateway / name / work-dir）原样保留。
func Restart() error {
	plistPath, err := PlistPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(plistPath); err != nil {
		if os.IsNotExist(err) {
			return ErrNotInstalled
		}
		return fmt.Errorf("读取 plist 失败: %w", err)
	}
	return bootstrap(plistPath)
}

// bootstrapAttempts / bootstrapRetryDelay 兜住 launchd 的老毛病：bootout 立刻返回，
// 但服务真正卸载是异步的，紧跟的 bootstrap 会撞上"上一个还没退干净"报
// `Bootstrap failed: 5: Input/output error`。重试几次即自愈，比让用户重敲一遍 start 好。
var (
	bootstrapAttempts   = 3
	bootstrapRetryDelay = 400 * time.Millisecond
)

// bootstrap 是 bootout → bootstrap → kickstart 三步的收口：
// bootout 失败一律忽略（未加载时 launchctl 就报错，这是正常路径），
// 后两步失败必须上抛并带上 launchctl 原文——用户据此判断是权限还是 plist 问题。
func bootstrap(plistPath string) error {
	bootout()
	var lastErr error
	var lastOutput string
	for attempt := range bootstrapAttempts {
		if attempt > 0 {
			time.Sleep(bootstrapRetryDelay)
		}
		lastOutput, lastErr = runLaunchctl("bootstrap", domainTarget(), plistPath)
		if lastErr == nil {
			break
		}
	}
	if lastErr != nil {
		return fmt.Errorf("launchctl bootstrap 失败(重试 %d 次): %w%s", bootstrapAttempts, lastErr, detail(lastOutput))
	}
	if output, err := runLaunchctl("kickstart", serviceTarget()); err != nil {
		return fmt.Errorf("launchctl kickstart 失败: %w%s", err, detail(output))
	}
	return nil
}

// bootout 尽力卸载服务，忽略错误（服务不在时 launchctl 必然报错）。
func bootout() {
	_, _ = runLaunchctl("bootout", serviceTarget())
}

func detail(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	return ": " + output
}

var (
	launchctlPID      = regexp.MustCompile(`"PID"\s*=\s*(\d+);`)
	launchctlLastExit = regexp.MustCompile(`"LastExitStatus"\s*=\s*(-?\d+);`)
)

// Query 汇报当前托管状态：plist 提供「配置成什么样」，launchctl list 提供「现在活没活」。
func Query() (Status, error) {
	plistPath, err := PlistPath()
	if err != nil {
		return Status{}, err
	}
	status := Status{Label: Label, PlistPath: plistPath}

	document, err := os.ReadFile(plistPath)
	switch {
	case err == nil:
		status.Installed = true
		// plist 读不动不该让 status 整体失败——服务可能正常跑着，
		// 此处只是少显示 Command / Logs 两行。
		if values, arrays, parseErr := parsePlist(document); parseErr == nil {
			status.Command = arrays["ProgramArguments"]
			status.LogPath = values["StandardOutPath"]
		}
	case os.IsNotExist(err): // 未托管，保持零值
	default:
		return status, fmt.Errorf("读取 plist 失败: %w", err)
	}

	output, err := runLaunchctl("list", Label)
	if err != nil {
		return status, nil // 未加载：launchctl 对未知 service 返回非零，不是故障
	}
	status.Loaded = true
	if pid, ok := intField(launchctlPID, output); ok {
		status.PID = pid
		status.Running = pid > 0
	}
	if code, ok := intField(launchctlLastExit, output); ok {
		status.LastExitStatus = code
		status.HasLastExit = true
	}
	return status, nil
}

func intField(pattern *regexp.Regexp, output string) (int, bool) {
	match := pattern.FindStringSubmatch(output)
	if match == nil {
		return 0, false
	}
	value, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, false
	}
	return value, true
}

// plistNode 是 plist 顶层 dict 的一个子节点。plist 用「<key> 与其后兄弟节点配对」
// 表达键值，encoding/xml 无法直接映射成 struct 字段，故按节点序列自行配对。
type plistNode struct {
	XMLName  xml.Name
	Chardata string   `xml:",chardata"`
	Strings  []string `xml:"string"`
}

type plistDocument struct {
	Dict struct {
		Nodes []plistNode `xml:",any"`
	} `xml:"dict"`
}

// parsePlist 只用于读回本包自己写出的 plist（顶层扁平 dict）：
// 按序把 <key> 与其后节点配对，嵌套 <dict>（EnvironmentVariables）整体跳过，
// 因而不会被内层的键名污染。不是通用 plist 解析器。
func parsePlist(document []byte) (map[string]string, map[string][]string, error) {
	var parsed plistDocument
	if err := xml.Unmarshal(document, &parsed); err != nil {
		return nil, nil, fmt.Errorf("解析 plist 失败: %w", err)
	}
	values := map[string]string{}
	arrays := map[string][]string{}
	key := ""
	for _, node := range parsed.Dict.Nodes {
		switch node.XMLName.Local {
		case "key":
			key = strings.TrimSpace(node.Chardata)
		case "string":
			if key != "" {
				values[key] = node.Chardata
			}
			key = ""
		case "array":
			if key != "" {
				arrays[key] = node.Strings
			}
			key = ""
		default:
			key = ""
		}
	}
	return values, arrays, nil
}
