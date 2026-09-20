// Package cli 是 vulhub-cli 的命令层。
//
// Run 是整个工具唯一的测试缝：它下面的东西都是实现细节。
package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/polite-007/vulhub-cli/internal/catalog"
	"github.com/polite-007/vulhub-cli/internal/compose"
	"github.com/polite-007/vulhub-cli/internal/envindex"
	"github.com/polite-007/vulhub-cli/internal/gitx"
	"github.com/polite-007/vulhub-cli/internal/hostinfo"
	"github.com/polite-007/vulhub-cli/internal/vulfocus"
)

// 退出码。
const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
	// ExitInterrupted 是 Ctrl+C 打断时的退出码，遵循 shell 惯例（128 + SIGINT）。
	ExitInterrupted = 130
)

// cloneDepth 是 init 的浅克隆深度：体积接近 tarball，同时保留 git 元信息，
// 使 update 能用 git pull 而非重新下载整包。
const cloneDepth = 1

// Deps 汇集全部外部依赖与配置。
type Deps struct {
	Compose compose.Runner
	Git     gitx.Runner
	Host    hostinfo.Info

	// VulhubRoot 是 vulhub 检出的根目录。
	VulhubRoot string
	// VulfocusRoot 是 vulfocus 来源的合成 compose 文件所在目录。
	VulfocusRoot string
	// IndexPath 是索引文件的位置，必须在 vulhub 检出之外。
	IndexPath string
	// RepoURL 是 vulhub 的上游地址。
	RepoURL string
	// VulfocusCatalog 覆盖二进制内嵌的 vulfocus 镜像清单。
	// 为 nil 时用内嵌的那份；测试用它构造一份可控的清单，
	// 否则每个测试都会被几百个真实镜像带着跑。
	VulfocusCatalog []vulfocus.Image
}

// vulfocusImages 返回当前生效的 vulfocus 镜像清单。
func (a *app) vulfocusImages() ([]vulfocus.Image, error) {
	if a.deps.VulfocusCatalog != nil {
		return a.deps.VulfocusCatalog, nil
	}
	return vulfocus.Images()
}

// Run 是命令行入口。args 不含程序名。
func Run(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer, isTTY bool, deps Deps) int {
	a := &app{ctx: ctx, deps: deps, in: in, out: out, errOut: errOut, isTTY: isTTY}

	if len(args) == 0 {
		a.printUsage(errOut)
		return ExitUsage
	}

	command, rest := args[0], args[1:]

	var code int
	switch command {
	case "init":
		code = a.cmdInit(rest)
	case "ls":
		code = a.cmdLs(rest)
	case "search":
		code = a.cmdSearch(rest)
	case "up":
		code = a.cmdUp(rest)
	case "stop":
		code = a.cmdStop(rest)
	case "del":
		code = a.cmdDel(rest)
	case "status":
		code = a.cmdStatus(rest)
	case "pull":
		code = a.cmdPull(rest)
	case "update":
		code = a.cmdUpdate(rest)
	case "gen-vulfocus":
		code = a.cmdGenVulfocus(rest)
	case "help", "-h", "--help":
		a.printUsage(out)
		code = ExitOK
	default:
		fmt.Fprintf(errOut, "未知命令 %q\n\n", command)
		a.printUsage(errOut)
		code = ExitUsage
	}

	if ctx.Err() != nil {
		// Ctrl+C 打断。命令自己的错误码此时多半只是打断的副产物，
		// 统一按 shell 惯例返回 130。
		return ExitInterrupted
	}
	return code
}

// app 持有一轮命令执行的上下文。
type app struct {
	ctx    context.Context
	deps   Deps
	in     io.Reader
	out    io.Writer
	errOut io.Writer
	isTTY  bool
	br     *bufio.Reader // 输入流上唯一的缓冲读取器，惰性建立
}

// target 是解析后的靶场。
type target struct {
	Number int
	Path   string
	Name   string
}

// absDir 返回靶场在磁盘上的绝对路径。
//
// 两个来源落在不同的根目录下：vulhub 的靶场是检出里的目录，
// vulfocus 的靶场是本地合成的目录。
func (a *app) absDir(path string) string {
	if vulfocus.IsVulfocus(path) {
		return filepath.Join(a.deps.VulfocusRoot, vulfocus.ImageName(path))
	}
	return filepath.Join(a.deps.VulhubRoot, filepath.FromSlash(path))
}

// state 是命令运行所需的靶场清单与索引。
type state struct {
	Environments []catalog.Environment
	Index        *envindex.Index

	byPath map[string]catalog.Environment
}

// load 读取靶场清单与索引，并为尚未分配编号的靶场补上编号。
//
// 补编号发生在解析之前，因此索引不存在时用编号操作也能正常工作——
// 多敲一次命令的摩擦换不来任何安全性，编号是可重建的派生数据。
func (a *app) load() (*state, error) { return a.loadWith(true) }

// loadForInit 与 load 相同，但不就索引重建发出警告：
// init 刚克隆完，索引本就不存在，那时的提示纯属噪音。
func (a *app) loadForInit() (*state, error) { return a.loadWith(false) }

func (a *app) loadWith(warnOnRebuild bool) (*state, error) {
	envs, err := catalog.Load(a.deps.VulhubRoot)
	if err != nil {
		// 检出不存在不是错误：vulfocus 来源的靶场内嵌在二进制里，
		// 没有 vulhub 检出它们照样可用。其他失败仍然要报出来。
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		envs = nil
	}

	vulfocusImages, err := a.vulfocusImages()
	if err != nil {
		return nil, err
	}
	for _, e := range vulfocus.EnvironmentsOf(vulfocusImages) {
		envs = append(envs, catalog.Environment{Path: e.Path, Name: e.Title, CreatedAt: e.CreatedAt})
	}
	sort.Slice(envs, func(i, j int) bool { return envs[i].Path < envs[j].Path })

	idx, status, err := envindex.Load(a.deps.IndexPath)
	if err != nil {
		return nil, err
	}
	if idx.Ensure(paths(envs)) {
		if err := idx.Save(a.deps.IndexPath); err != nil {
			return nil, err
		}
		if warnOnRebuild {
			a.warnIndexRebuild(status)
		}
	}

	byPath := make(map[string]catalog.Environment, len(envs))
	for _, e := range envs {
		byPath[e.Path] = e
	}
	return &state{Environments: envs, Index: idx, byPath: byPath}, nil
}

// warnIndexRebuild 在索引被重建时告知用户编号已重新分配。
//
// 这是编号方案固有的弱点：索引是唯一真相源，丢了编号就全乱。
// 用户必须知道自己此前记住的编号不再有效，否则会安静地用错靶场。
func (a *app) warnIndexRebuild(status envindex.Status) {
	switch status {
	case envindex.StatusCorrupt:
		fmt.Fprintf(a.errOut, "警告：索引文件 %s 无法解析，已重新建立。编号已重新分配，此前记住的编号不再有效。\n", a.deps.IndexPath)
	case envindex.StatusMissing:
		fmt.Fprintf(a.errOut, "提示：未找到索引文件 %s，已新建并分配编号。\n", a.deps.IndexPath)
	}
}

func paths(envs []catalog.Environment) []string {
	out := make([]string, 0, len(envs))
	for _, e := range envs {
		out = append(out, e.Path)
	}
	return out
}

// resolve 把命令行上的一个靶场参数解析成靶场。参数可以是编号或路径。
func (s *state) resolve(arg string) (target, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return target{}, errors.New("靶场参数为空")
	}

	// 纯数字视为编号。
	if n, err := strconv.Atoi(arg); err == nil {
		path, ok := s.Index.PathByNumber(n)
		if !ok {
			return target{}, fmt.Errorf("编号 %d 尚未分配", n)
		}
		env, ok := s.byPath[path]
		if !ok {
			return target{}, fmt.Errorf("编号 %d 对应的靶场 %s 已不在 vulhub 检出中，请运行 update", n, path)
		}
		return target{Number: n, Path: env.Path, Name: env.Name}, nil
	}

	normalized := filepath.ToSlash(strings.TrimSuffix(arg, "/"))
	env, ok := s.byPath[normalized]
	if !ok {
		return target{}, fmt.Errorf("找不到靶场 %s", arg)
	}
	number, _ := s.Index.Number(env.Path)
	return target{Number: number, Path: env.Path, Name: env.Name}, nil
}

// splitList 把 "1,2,68" 这样的逗号列表摊平，并跳过空项。
func splitList(args []string) []string {
	var out []string
	for _, arg := range args {
		for _, part := range strings.Split(arg, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// requireTarget 确认恰好给出了一个靶场参数。
func requireTarget(args []string) error {
	if len(args) == 0 {
		return errors.New("没有给出靶场")
	}
	return nil
}

// parseFlags 摘出已知开关，返回其余的位置参数。
//
// "--" 之后的参数一律视为位置参数；未知的 -x 报错；单独的 "-" 按位置参数处理。
func parseFlags(args []string, flags map[string]*bool) ([]string, error) {
	var positional []string
	for i, arg := range args {
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if target, ok := flags[arg]; ok {
			*target = true
			continue
		}
		if strings.HasPrefix(arg, "-") && arg != "-" {
			return nil, fmt.Errorf("未知选项 %s", arg)
		}
		positional = append(positional, arg)
	}
	return positional, nil
}

// resolveAll 解析一串靶场参数，任一失败即整体失败。
func (a *app) resolveAll(args []string) ([]target, error) {
	s, err := a.load()
	if err != nil {
		return nil, err
	}
	items := splitList(args)
	if len(items) == 0 {
		return nil, errors.New("没有给出靶场")
	}
	targets := make([]target, 0, len(items))
	for _, item := range items {
		t, err := s.resolve(item)
		if err != nil {
			return nil, err
		}
		targets = append(targets, t)
	}
	return targets, nil
}

// errorf 向 stderr 报告错误并返回退出码。
func (a *app) errorf(format string, args ...any) int {
	fmt.Fprintf(a.errOut, format+"\n", args...)
	return ExitError
}

// usageError 报告一个参数错误并打印用法。
func (a *app) usageError(err error) int {
	fmt.Fprintf(a.errOut, "%v\n\n", err)
	a.printUsage(a.errOut)
	return ExitUsage
}

// fail 把一个错误报告到 stderr。
func (a *app) fail(err error) int {
	return a.errorf("%v", err)
}

func (a *app) printUsage(w io.Writer) {
	fmt.Fprint(w, `vulhub-cli —— 管理 vulhub 靶场

用法：
  vulhub <命令> [参数]

命令：
  init                              拉取 vulhub 到 ~/vulhub 并建立索引
  update                            更新 vulhub 检出
  ls [--json] [-time]               列出全部靶场
  search <关键词>… [--json] [-time] 按漏洞标题与靶场路径检索靶场
  up <靶场>[,…]                     启动靶场
  stop <靶场>[,…]                   停止靶场
  del <靶场> [-a] [-y]              销毁靶场
  status [--json]                   查看运行中的靶场
  pull <靶场>[,…] | --all           拉取靶场镜像

<靶场> 可以是编号（68）或路径（activemq/CVE-2023-46604）。
靶场有两个来源：vulhub 仓库（路径形如 activemq/CVE-2023-46604）
与 Docker Hub 的 vulfocus（路径形如 vulfocus/drupal-cve_2018_7600）。
逗号分隔的多选只适用于 pull、up、stop；del 一次只能销毁一个靶场。

-time 按创建时间倒序显示（新的在前）。它只影响显示，不影响编号——
编号按路径字典序一次性分配、此后不变，这是"记住 68 号"能成立的前提。
vulhub 来源没有创建时间（上游注册表里没有日期字段），按时间排序时排在最后。

del 的选项：
  -a   连同该靶场引用的镜像与该靶场的 volume 一起删除
  -y   跳过二次确认

维护者命令：
  gen-vulfocus [-o 路径]            从 Docker Hub 重新生成 vulfocus 镜像清单
`)
}
