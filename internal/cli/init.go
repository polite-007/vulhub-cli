package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// cmdInit 拉取 vulhub 到本地并建立索引。
func (a *app) cmdInit(args []string) int {
	if len(args) > 0 {
		a.printUsage(a.errOut)
		return ExitUsage
	}

	// 前置检查：git 与 docker 必须存在，compose 的两种调用方式至少有一个可用。
	if err := a.deps.Git.Check(a.ctx); err != nil {
		return a.fail(err)
	}
	if err := a.deps.Compose.Check(a.ctx); err != nil {
		return a.fail(err)
	}

	if _, err := os.Stat(a.deps.VulhubRoot); err == nil {
		return a.errorf("%s 已存在，若要更新请运行 vulhub update", a.deps.VulhubRoot)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return a.fail(err)
	}

	if err := os.MkdirAll(filepath.Dir(a.deps.VulhubRoot), 0o755); err != nil {
		return a.fail(err)
	}

	fmt.Fprintf(a.out, "正在拉取 vulhub 到 %s …\n", a.deps.VulhubRoot)
	if err := a.deps.Git.Clone(a.ctx, a.deps.RepoURL, a.deps.VulhubRoot, cloneDepth); err != nil {
		return a.fail(err)
	}

	// 克隆完成后立即分配编号：init 是一个"做准备"的动作，
	// 用户预期它把事情都办妥，不该等到第一次 ls 才有编号可用。
	s, err := a.loadForInit()
	if err != nil {
		return a.fail(err)
	}
	fmt.Fprintf(a.out, "已就绪：共 %d 个靶场\n", len(s.Environments))
	return ExitOK
}
