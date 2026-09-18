package cli

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/polite-007/vulhub-cli/internal/compose"
)

// statusJSON 是 status --json 的输出单元。
type statusJSON struct {
	Number     int             `json:"number"`
	Path       string          `json:"path"`
	Name       string          `json:"name"`
	Containers []containerJSON `json:"containers"`
}

type containerJSON struct {
	Name    string     `json:"name"`
	Service string     `json:"service"`
	State   string     `json:"state"`
	Ports   []portJSON `json:"ports"`
}

type portJSON struct {
	HostPort      int    `json:"host_port"`
	ContainerPort int    `json:"container_port"`
	Protocol      string `json:"protocol"`
}

// runningGroup 是某个靶场的一组运行中容器。
type runningGroup struct {
	Path       string
	Containers []compose.Container
}

// cmdStatus 显示当前运行中的靶场。
func (a *app) cmdStatus(args []string) int {
	jsonOut, rest, err := splitJSONFlag(args)
	if err != nil {
		return a.usageError(err)
	}
	if len(rest) > 0 {
		a.printUsage(a.errOut)
		return ExitUsage
	}

	containers, err := a.deps.Compose.Containers(a.ctx, "")
	if err != nil {
		return a.fail(err)
	}

	// 靶场清单只用于显示标题与编号。vulhub 尚未初始化时 status 仍然应该能用，
	// 因此清单读取失败不构成错误，退化为只按工作目录展示。
	s, err := a.load()
	if err != nil {
		s = &state{}
	}

	groups := a.groupByEnvironment(containers)

	if jsonOut {
		payload := make([]statusJSON, 0, len(groups))
		for _, g := range groups {
			payload = append(payload, a.groupJSON(g, s))
		}
		if err := writeJSON(a.out, payload); err != nil {
			return a.fail(err)
		}
		return ExitOK
	}

	if len(groups) == 0 {
		fmt.Fprintln(a.out, "当前没有运行中的靶场")
		return ExitOK
	}

	fmt.Fprintln(a.out, "运行中的靶场：")
	for _, g := range groups {
		number, name := a.describe(g.Path, s)
		line := fmt.Sprintf("\n%4d  %s", number, g.Path)
		if name != "" {
			line += "  " + name
		}
		fmt.Fprintln(a.out, line)
		for _, c := range g.Containers {
			fmt.Fprintf(a.out, "        %-40s %s%s\n", c.Name, c.State, formatPorts(c.Ports))
		}
	}
	return ExitOK
}

// groupByEnvironment 把容器按所属靶场分组，按靶场路径排序。
func (a *app) groupByEnvironment(containers []compose.Container) []runningGroup {
	byPath := map[string][]compose.Container{}
	for _, c := range containers {
		path := a.pathForWorkDir(c.WorkDir)
		byPath[path] = append(byPath[path], c)
	}

	groups := make([]runningGroup, 0, len(byPath))
	for path, cs := range byPath {
		groups = append(groups, runningGroup{Path: path, Containers: cs})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Path < groups[j].Path })
	return groups
}

// pathForWorkDir 把容器标签里的工作目录还原成靶场路径。
// 工作目录不在 vulhub 检出内时，原样返回它，免得容器凭空消失。
func (a *app) pathForWorkDir(workDir string) string {
	if workDir == "" {
		return "(未知靶场)"
	}
	rel, err := filepath.Rel(a.deps.VulhubRoot, workDir)
	if err != nil || strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(workDir)
	}
	return filepath.ToSlash(rel)
}

func (a *app) describe(path string, s *state) (int, string) {
	for _, e := range s.Environments {
		if e.Path == path {
			if s.Index == nil {
				return 0, e.Name
			}
			number, _ := s.Index.Number(e.Path)
			return number, e.Name
		}
	}
	return 0, ""
}

func (a *app) groupJSON(g runningGroup, s *state) statusJSON {
	number, name := a.describe(g.Path, s)
	out := statusJSON{Number: number, Path: g.Path, Name: name, Containers: []containerJSON{}}
	for _, c := range g.Containers {
		item := containerJSON{Name: c.Name, Service: c.Service, State: c.State, Ports: []portJSON{}}
		for _, p := range c.Ports {
			item.Ports = append(item.Ports, portJSON{
				HostPort:      p.HostPort,
				ContainerPort: p.ContainerPort,
				Protocol:      p.Protocol,
			})
		}
		out.Containers = append(out.Containers, item)
	}
	return out
}

// formatPorts 渲染容器的端口映射，没有映射时返回空串。
func formatPorts(ports []compose.PortMapping) string {
	if len(ports) == 0 {
		return ""
	}
	parts := make([]string, 0, len(ports))
	for _, p := range ports {
		parts = append(parts, fmt.Sprintf("%d→%d/%s", p.HostPort, p.ContainerPort, p.Protocol))
	}
	return "  " + strings.Join(parts, ", ")
}
