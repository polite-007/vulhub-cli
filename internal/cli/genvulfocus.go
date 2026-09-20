package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/polite-007/vulhub-cli/internal/vulfocus"
)

// defaultVulfocusDataPath 是镜像清单在仓库中的位置，相对于当前工作目录。
// 它必须在 vulfocus 包目录内，go:embed 无法引用父目录。
const defaultVulfocusDataPath = "internal/vulfocus/data/vulfocus.json"

// cmdGenVulfocus 从 Docker Hub 重新生成 vulfocus 镜像清单。
//
// 这是维护者的命令：清单随二进制分发，用户侧的 init 与 update 都不碰网络。
// 取舍见 docs/adr/0002-embedded-vulfocus-catalog.md。
func (a *app) cmdGenVulfocus(args []string) int {
	opts, err := parseGenArgs(args)
	if err != nil {
		return a.usageError(err)
	}

	if opts.username == "" || opts.token == "" {
		fmt.Fprintln(a.errOut, "提示：未提供 Docker Hub 凭证。匿名请求只能翻到第 100 个镜像，"+
			"而 vulfocus 命名空间有四百多个，因此无法生成完整清单。请用 --username/--token 提供凭证。")
	} else {
		fmt.Fprintf(a.out, "已使用 Docker Hub 账户 %s 认证\n", opts.username)
	}
	fmt.Fprintln(a.out, "正在从 Docker Hub 读取 vulfocus 的镜像清单…")

	gen := &vulfocus.Generator{
		OnProgress: a.progressReporter(),
		Username:   opts.username,
		Password:   opts.token,
	}
	images, skipped, err := gen.Generate(a.ctx)
	if err != nil {
		return a.fail(err)
	}
	if a.isTTY {
		fmt.Fprintln(a.out) // 结束进度行
	}

	// 跳过的镜像必须报出来：一份"看起来正常但悄悄少了几个"的清单，
	// 比直接失败更难被发现。
	if len(skipped) > 0 {
		fmt.Fprintf(a.errOut, "\n以下 %d 个镜像不可用，已跳过：\n", len(skipped))
		for _, s := range skipped {
			fmt.Fprintf(a.errOut, "  %s：%s\n", s.Image, s.Reason)
		}
	}

	raw, err := json.MarshalIndent(images, "", "  ")
	if err != nil {
		return a.fail(err)
	}
	if err := os.MkdirAll(filepath.Dir(opts.outPath), 0o755); err != nil {
		return a.fail(err)
	}
	if err := os.WriteFile(opts.outPath, append(raw, '\n'), 0o644); err != nil {
		return a.fail(err)
	}

	fmt.Fprintf(a.out, "已写入 %s：%d 个镜像\n", opts.outPath, len(images))
	return ExitOK
}

// progressReporter 在终端上原地刷新进度；不是终端时按 25 个一次输出，
// 免得往日志里灌几百行。
func (a *app) progressReporter() func(done, total int, image string) {
	if a.isTTY {
		return func(done, total int, image string) {
			fmt.Fprintf(a.out, "\r[%d/%d] %-50s", done, total, image)
		}
	}
	return func(done, total int, image string) {
		if done%25 == 0 || done == total {
			fmt.Fprintf(a.out, "  %d/%d\n", done, total)
		}
	}
}

// genOptions 是 gen-vulfocus 的参数。
type genOptions struct {
	outPath  string
	username string
	token    string
}

// parseGenArgs 解析参数。凭证也可以从环境变量取，免得出现在命令历史里。
func parseGenArgs(args []string) (genOptions, error) {
	opts := genOptions{
		outPath:  defaultVulfocusDataPath,
		username: os.Getenv("DOCKERHUB_USERNAME"),
		token:    os.Getenv("DOCKERHUB_TOKEN"),
	}

	value := func(i *int, name string) (string, error) {
		if *i+1 >= len(args) {
			return "", fmt.Errorf("%s 需要一个参数", name)
		}
		*i++
		return args[*i], nil
	}

	for i := 0; i < len(args); i++ {
		var err error
		switch args[i] {
		case "-o", "--output":
			opts.outPath, err = value(&i, args[i])
		case "--username":
			opts.username, err = value(&i, args[i])
		case "--token":
			opts.token, err = value(&i, args[i])
		default:
			return genOptions{}, fmt.Errorf("未知选项 %s", args[i])
		}
		if err != nil {
			return genOptions{}, err
		}
	}
	return opts, nil
}
