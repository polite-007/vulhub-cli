package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// cmdUpdate 更新 vulhub 检出。
func (a *app) cmdUpdate(args []string) int {
	if len(args) > 0 {
		a.printUsage(a.errOut)
		return ExitUsage
	}
	if err := a.deps.Git.Check(a.ctx); err != nil {
		return a.fail(err)
	}
	if _, err := os.Stat(a.deps.VulhubRoot); err != nil {
		return a.errorf("%s 不存在，请先运行 vulhub init", a.deps.VulhubRoot)
	}

	before, _ := a.deps.Git.Head(a.ctx, a.deps.VulhubRoot)

	// 拉取失败时返回的错误里带有 git 的原始输出：本地改动导致的冲突
	// 由用户自己判断怎么处理，工具不替他做合并、stash 或覆盖的决策。
	if err := a.deps.Git.Pull(a.ctx, a.deps.VulhubRoot); err != nil {
		return a.fail(err)
	}

	after, _ := a.deps.Git.Head(a.ctx, a.deps.VulhubRoot)

	// 重新读取清单并补编号：本次拉取新增的靶场会被追加到编号末尾。
	s, err := a.load()
	if err != nil {
		return a.fail(err)
	}
	fmt.Fprintf(a.out, "vulhub 已更新：共 %d 个靶场\n", len(s.Environments))

	changed, err := a.deps.Git.ChangedPaths(a.ctx, a.deps.VulhubRoot, before, after)
	if err != nil {
		changed = nil
	}
	a.reportAffectedRunning(changed, s)
	return ExitOK
}

// reportAffectedRunning 列出受本次更新影响的运行中靶场。
//
// update 不阻止正在运行的靶场，但用户需要知道哪些环境的内容已经变了、
// 继续用下去会和他看到的文件对不上。
func (a *app) reportAffectedRunning(changed []string, s *state) {
	containers, err := a.deps.Compose.Containers(a.ctx, "")
	if err != nil || len(containers) == 0 {
		return
	}

	exists := make(map[string]bool, len(s.Environments))
	for _, e := range s.Environments {
		exists[e.Path] = true
	}

	// 只认检出里的 compose 项目；这台机器上别人的项目不是靶场，不该被报进来。
	running := map[string]bool{}
	for _, c := range containers {
		if rel, ok := a.relativeToCheckout(c.WorkDir); ok {
			running[rel] = true
		}
	}
	if len(running) == 0 {
		return
	}

	affected := map[string]bool{}
	for path := range running {
		if !exists[path] {
			// 靶场已被本次更新删除，它一定受影响。
			affected[path] = true
			continue
		}
		for _, file := range changed {
			file = filepath.ToSlash(file)
			if file == path || strings.HasPrefix(file, path+"/") {
				affected[path] = true
				break
			}
		}
	}
	if len(affected) == 0 {
		return
	}

	paths := make([]string, 0, len(affected))
	for p := range affected {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	fmt.Fprintln(a.out, "\n以下运行中的靶场受本次更新影响，建议重启：")
	for _, p := range paths {
		fmt.Fprintf(a.out, "  %s\n", p)
	}
}
