package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/polite-007/vulhub-cli/internal/catalog"
	"github.com/polite-007/vulhub-cli/internal/pager"
)

// environmentJSON 是 ls 与 search 的 --json 输出单元。
type environmentJSON struct {
	Number int    `json:"number"`
	Path   string `json:"path"`
	Name   string `json:"name"`
}

// cmdLs 列出全部靶场。
func (a *app) cmdLs(args []string) int {
	var jsonOut, byTime bool
	rest, err := parseFlags(args, map[string]*bool{
		"--json": &jsonOut,
		"-time":  &byTime,
		"--time": &byTime,
	})
	if err != nil {
		return a.usageError(err)
	}
	if len(rest) > 0 {
		return a.usageError(errors.New("ls 不接受位置参数"))
	}

	s, err := a.load()
	if err != nil {
		return a.fail(err)
	}
	return a.printEnvironments(s, s.Environments, jsonOut, byTime)
}

// cmdSearch 按漏洞标题与靶场路径检索靶场。
func (a *app) cmdSearch(args []string) int {
	var jsonOut, byTime bool
	keywords, err := parseFlags(args, map[string]*bool{
		"--json": &jsonOut,
		"-time":  &byTime,
		"--time": &byTime,
	})
	if err != nil {
		return a.usageError(err)
	}
	if len(keywords) == 0 {
		return a.usageError(errors.New("用法：vulhub search <关键词>…"))
	}

	s, err := a.load()
	if err != nil {
		return a.fail(err)
	}

	var matches []catalog.Environment
	for _, env := range s.Environments {
		if matchesAll(env, keywords) {
			matches = append(matches, env)
		}
	}

	if code := a.printEnvironments(s, matches, jsonOut, byTime); code != ExitOK {
		return code
	}
	if len(matches) == 0 {
		// 无命中返回非零退出码，便于脚本判断"没找到"。
		return ExitError
	}
	return ExitOK
}

func (a *app) printEnvironments(s *state, envs []catalog.Environment, jsonOut, byTime bool) int {
	if byTime {
		envs = sortedByTimeDesc(envs)
	}

	if jsonOut {
		payload := make([]environmentJSON, 0, len(envs))
		for _, env := range envs {
			number, _ := s.Index.Number(env.Path)
			payload = append(payload, environmentJSON{Number: number, Path: env.Path, Name: env.Name})
		}
		if err := writeJSON(a.out, payload); err != nil {
			return a.fail(err)
		}
		return ExitOK
	}

	p := pager.New(a.ctx, a.out, a.in, a.isTTY)
	width := 0
	for _, env := range envs {
		if len(env.Path) > width {
			width = len(env.Path)
		}
	}
	for _, env := range envs {
		number, _ := s.Index.Number(env.Path)
		if err := p.Println(formatEnvironment(number, env.Path, env.Name, width)); err != nil {
			if errors.Is(err, pager.ErrInterrupted) {
				return ExitInterrupted
			}
			return a.fail(err)
		}
	}
	return ExitOK
}

func formatEnvironment(number int, path, name string, width int) string {
	line := fmt.Sprintf("%4d  %-*s", number, width, path)
	if name != "" {
		line += "  " + name
	}
	return line
}

// matchesAll 判定靶场是否命中全部关键词。
//
// 检索 name 与 path 两个字段：path 恰好包含软件名与 CVE 号，
// 因此软件名、CVE 号、漏洞标题三类最常见的检索词被两个字段全覆盖，
// 且在 environments.toml 缺失的降级路径下同样可用。
func matchesAll(env catalog.Environment, keywords []string) bool {
	haystack := strings.ToLower(env.Name + "\n" + env.Path)
	for _, kw := range keywords {
		if !strings.Contains(haystack, strings.ToLower(kw)) {
			return false
		}
	}
	return true
}

// sortedByTimeDesc 按创建时间倒序排一份副本，新的在前。
//
// 它只影响显示，不影响编号——编号按路径字典序一次性分配、此后不变，
// 这是"记住 68 号"这件事能成立的前提。
//
// **没有创建时间的靶场排在最后**（vulhub 来源全都没有：environments.toml
// 里没有任何日期字段），同一时间点内按路径排序以保证结果稳定。
func sortedByTimeDesc(envs []catalog.Environment) []catalog.Environment {
	out := append([]catalog.Environment(nil), envs...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].CreatedAt, out[j].CreatedAt
		switch {
		case a.IsZero() && b.IsZero():
			return out[i].Path < out[j].Path
		case a.IsZero():
			return false
		case b.IsZero():
			return true
		case !a.Equal(b):
			return a.After(b)
		default:
			return out[i].Path < out[j].Path
		}
	})
	return out
}

// splitJSONFlag 已由 parseFlags 取代。
func writeJSON(w io.Writer, payload any) error {
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(append(raw, '\n'))
	return err
}
