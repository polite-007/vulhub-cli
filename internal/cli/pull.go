package cli

import (
	"errors"
	"fmt"
)

func (a *app) cmdPull(args []string) int {
	var pullAll bool
	positional, err := parseFlags(args, map[string]*bool{"--all": &pullAll})
	if err != nil {
		return a.usageError(err)
	}
	if pullAll && len(positional) > 0 {
		return a.usageError(errors.New("不能同时指定靶场与 --all"))
	}
	if !pullAll && len(positional) == 0 {
		return a.usageError(errors.New("用法：vulhub pull <靶场>[,…] 或 vulhub pull --all"))
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
