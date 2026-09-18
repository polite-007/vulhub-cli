// Package compose 是对 docker compose 的封装。
//
// 工具通过调用 docker compose 命令行操作容器，不使用 Docker SDK。
// 运行期探测 `docker compose`（v2）与 `docker-compose`（v1）哪个可用。
package compose

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// docker compose 为每个容器打上的标签。
const (
	LabelProject = "com.docker.compose.project"
	LabelService = "com.docker.compose.service"
	LabelWorkDir = "com.docker.compose.project.working_dir"
)

// PortMapping 是一条端口映射。
type PortMapping struct {
	HostIP        string
	HostPort      int
	ContainerPort int
	Protocol      string
}

// Container 是一个由 compose 创建的容器。
type Container struct {
	ID      string
	Name    string
	Project string
	Service string
	WorkDir string
	State   string
	Ports   []PortMapping
}

// Runner 是编排端口，测试中以假对象替换。
type Runner interface {
	// Check 确认 docker 与 compose 可用，不可用时返回说明缺什么的错误。
	Check(ctx context.Context) error
	Up(ctx context.Context, dir string) error
	Stop(ctx context.Context, dir string) error
	Down(ctx context.Context, dir string, removeVolumes bool) error
	Pull(ctx context.Context, dir string) error
	// Images 返回某个靶场的 compose 文件引用到的镜像。
	Images(ctx context.Context, dir string) ([]string, error)
	// RemoveImages 删除镜像。失败是常见的（镜像被其他容器占用），由调用方决定如何处理。
	RemoveImages(ctx context.Context, images []string) error
	// Containers 返回全机由 compose 创建的容器；workDir 非空时只返回属于该靶场的容器。
	Containers(ctx context.Context, workDir string) ([]Container, error)
}

// DockerRunner 是 Runner 的真实实现。
type DockerRunner struct {
	// Stdout 与 Stderr 接收长耗时命令的输出，让用户能看到进度。为 nil 时丢弃。
	Stdout io.Writer
	Stderr io.Writer

	base []string // 探测到的 compose 调用方式，探测后缓存
}

// resolve 探测并缓存 compose 的调用方式。
func (r *DockerRunner) resolve(ctx context.Context) ([]string, error) {
	if r.base != nil {
		return r.base, nil
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, errors.New("找不到 docker 命令，请先安装 Docker")
	}
	if exec.CommandContext(ctx, "docker", "compose", "version").Run() == nil {
		r.base = []string{"docker", "compose"}
		return r.base, nil
	}
	if _, err := exec.LookPath("docker-compose"); err == nil {
		if exec.CommandContext(ctx, "docker-compose", "version").Run() == nil {
			r.base = []string{"docker-compose"}
			return r.base, nil
		}
	}
	return nil, errors.New("找不到 compose 命令，需要 `docker compose` 或 `docker-compose` 之一")
}

// Check 确认 docker 与 compose 可用。
func (r *DockerRunner) Check(ctx context.Context) error {
	_, err := r.resolve(ctx)
	return err
}

// Up 在后台启动靶场。
func (r *DockerRunner) Up(ctx context.Context, dir string) error {
	// 不做端口冲突预检：冲突交给 Docker 报错。
	return r.stream(ctx, dir, "up", "-d")
}

// Stop 停止靶场，容器与网络保留。
func (r *DockerRunner) Stop(ctx context.Context, dir string) error {
	return r.stream(ctx, dir, "stop")
}

// Down 删除靶场的容器与网络，removeVolumes 为真时连 volume 一起删。
func (r *DockerRunner) Down(ctx context.Context, dir string, removeVolumes bool) error {
	args := []string{"down"}
	if removeVolumes {
		args = append(args, "-v")
	}
	return r.stream(ctx, dir, args...)
}

// Pull 拉取靶场引用的镜像。
func (r *DockerRunner) Pull(ctx context.Context, dir string) error {
	return r.stream(ctx, dir, "pull")
}

// Images 返回靶场的 compose 文件引用到的镜像。
func (r *DockerRunner) Images(ctx context.Context, dir string) ([]string, error) {
	out, err := r.output(ctx, dir, "config", "--images")
	if err != nil {
		return nil, err
	}
	var images []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			images = append(images, line)
		}
	}
	return images, nil
}

// RemoveImages 删除镜像。
func (r *DockerRunner) RemoveImages(ctx context.Context, images []string) error {
	if len(images) == 0 {
		return nil
	}
	if _, err := r.resolve(ctx); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "docker", append([]string{"rmi"}, images...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("删除镜像: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Containers 返回全机由 compose 创建的容器。
func (r *DockerRunner) Containers(ctx context.Context, workDir string) ([]Container, error) {
	psArgs := []string{"ps", "-q", "--filter", "label=" + LabelProject}
	if workDir != "" {
		psArgs = append(psArgs, "--filter", "label="+LabelWorkDir+"="+workDir)
	}
	idsOut, err := r.dockerOutput(ctx, psArgs...)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, line := range strings.Split(idsOut, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			ids = append(ids, line)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}

	inspectOut, err := r.dockerOutput(ctx, append([]string{"inspect"}, ids...)...)
	if err != nil {
		return nil, err
	}
	return parseInspect(inspectOut)
}

// inspectResult 是 docker inspect 输出中我们用到的部分。
type inspectResult struct {
	ID    string `json:"Id"`
	Name  string `json:"Name"`
	State struct {
		Status string `json:"Status"`
	} `json:"State"`
	Config struct {
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	NetworkSettings struct {
		Ports map[string][]struct {
			HostIP   string `json:"HostIp"`
			HostPort string `json:"HostPort"`
		} `json:"Ports"`
	} `json:"NetworkSettings"`
}

func parseInspect(raw string) ([]Container, error) {
	var results []inspectResult
	if err := json.Unmarshal([]byte(raw), &results); err != nil {
		return nil, fmt.Errorf("解析 docker inspect 输出: %w", err)
	}

	containers := make([]Container, 0, len(results))
	for _, res := range results {
		c := Container{
			ID:      res.ID,
			Name:    strings.TrimPrefix(res.Name, "/"),
			Project: res.Config.Labels[LabelProject],
			Service: res.Config.Labels[LabelService],
			WorkDir: res.Config.Labels[LabelWorkDir],
			State:   res.State.Status,
		}
		c.Ports = parsePorts(res.NetworkSettings.Ports)
		containers = append(containers, c)
	}
	sort.Slice(containers, func(i, j int) bool { return containers[i].Name < containers[j].Name })
	return containers, nil
}

// parsePorts 把 inspect 的 Ports 映射（键形如 "80/tcp"）摊平成有序列表。
func parsePorts(ports map[string][]struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}) []PortMapping {
	var out []PortMapping
	for key, bindings := range ports {
		containerPort, protocol := splitPortKey(key)
		for _, b := range bindings {
			hostPort, err := strconv.Atoi(b.HostPort)
			if err != nil || hostPort == 0 {
				continue // 未发布到宿主
			}
			out = append(out, PortMapping{
				HostIP:        b.HostIP,
				HostPort:      hostPort,
				ContainerPort: containerPort,
				Protocol:      protocol,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].HostPort != out[j].HostPort {
			return out[i].HostPort < out[j].HostPort
		}
		return out[i].ContainerPort < out[j].ContainerPort
	})
	return out
}

func splitPortKey(key string) (int, string) {
	port, protocol, found := strings.Cut(key, "/")
	if !found {
		protocol = "tcp"
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		n = 0
	}
	return n, protocol
}

// stream 在靶场目录里执行 compose 子命令，输出直接转发给用户。
func (r *DockerRunner) stream(ctx context.Context, dir string, args ...string) error {
	base, err := r.resolve(ctx)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, base[0], append(append([]string{}, base[1:]...), args...)...)
	cmd.Dir = dir
	cmd.Stdout = r.Stdout
	cmd.Stderr = r.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// output 在靶场目录里执行 compose 子命令并捕获其输出。
func (r *DockerRunner) output(ctx context.Context, dir string, args ...string) (string, error) {
	base, err := r.resolve(ctx)
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, base[0], append(append([]string{}, base[1:]...), args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker compose %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// dockerOutput 执行 docker 子命令并捕获其输出。
func (r *DockerRunner) dockerOutput(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
