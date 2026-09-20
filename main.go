// Command vulhub-cli 管理 vulhub 靶场。
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/polite-007/vulhub-cli/internal/cli"
	"github.com/polite-007/vulhub-cli/internal/compose"
	"github.com/polite-007/vulhub-cli/internal/gitx"
	"github.com/polite-007/vulhub-cli/internal/hostinfo"
)

// defaultRepoURL 是 vulhub 的上游地址。
const defaultRepoURL = "https://github.com/vulhub/vulhub.git"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "找不到用户主目录：%v\n", err)
		os.Exit(cli.ExitError)
	}

	deps := cli.Deps{
		Compose: &compose.DockerRunner{Stdout: os.Stdout, Stderr: os.Stderr},
		Git:     &gitx.ExecRunner{Stdout: os.Stdout, Stderr: os.Stderr},
		Host:    hostinfo.System{},

		VulhubRoot: filepath.Join(home, "vulhub"),
		// 索引必须放在 vulhub 检出之外：检出是 git 仓库，
		// 索引写进去会产生未跟踪文件，使 update 的 git pull 面对脏工作区。
		IndexPath: filepath.Join(home, ".local", "share", "vulhub-cli", "index.json"),
		// vulfocus 来源的靶场没有目录，它们的 compose 文件由本工具合成到这里。
		// 同样不能写进 ~/vulhub。
		VulfocusRoot: filepath.Join(home, ".local", "share", "vulhub-cli", "vulfocus"),
		RepoURL:      defaultRepoURL,
	}

	os.Exit(cli.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, isTerminal(os.Stdout), deps))
}

// isTerminal 判断输出是否为终端。不是终端时分页必须关闭，
// 否则 `vulhub ls | grep xxx` 会挂住。
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
