package cli

import (
	"fmt"
	"strings"
)

// cmdPull 拉取靶场镜像。
func (a *app) cmdPull(args []string) int {
	pullAll, positional, err := parsePullArgs(args)
	if err != nil {
		return a.fail(err)
	}
	if pullAll && len(positional) > 0 {
		return a.errorf("不能同时指定靶场与 --all")
	}
	if !pullAll && len(positional) == 0 {
		fmt.Fprint(a.errOut, "用法：vulhub pull <靶场>[,…] 或 vulhub pull --all\n")
		return ExitUsage
	}
	if err := a.deps.Compose.Check(a.ctx); err != nil {
		return a.fail(err)
	}

	// --all 必须显式给出：靶场镜像普遍 100MB~1GB，全量是几十 GB 级别，
	// 默认触发是灾难。它的真实用途是"要断网演练了，先把要用的都拉下来"。
	if pullAll {
		s, err := a.load()
		if err != nil {
			return a.fail(err)
		}
		for _, env := range s.Environments {
			fmt.Fprintf(a.out, "正在拉取 %s …\n", env.Path)
			if err := a.deps.Compose.Pull(a.ctx, a.absDir(env.Path)); err != nil {
				return a.fail(err)
			}
		}
		return ExitOK
	}

	targets, err := a.resolveAll(positional)
	if err != nil {
		return a.fail(err)
	}
	for _, t := range targets {
		fmt.Fprintf(a.out, "正在拉取 %d  %s …\n", t.Number, t.Path)
		if err := a.deps.Compose.Pull(a.ctx, a.absDir(t.Path)); err != nil {
			return a.fail(err)
		}
	}
	return ExitOK
}

func parsePullArgs(args []string) (pullAll bool, positional []string, err error) {
	for _, arg := range args {
		switch arg {
		case "--all":
			pullAll = true
		default:
			if strings.HasPrefix(arg, "-") && arg != "-" {
				return false, nil, fmt.Errorf("pull 不支持选项 %s", arg)
			}
			positional = append(positional, arg)
		}
	}
	return pullAll, positional, nil
}
