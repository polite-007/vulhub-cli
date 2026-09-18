// Package gitx 是对 git 的封装。
package gitx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// Runner 是 git 端口，测试中以假对象替换。
type Runner interface {
	// Check 确认 git 可用。
	Check(ctx context.Context) error
	// Clone 拉取仓库到 dest。depth 大于 0 时做浅克隆。
	Clone(ctx context.Context, url, dest string, depth int) error
	// Pull 在 dir 里执行 git pull。遇到冲突时返回的错误必须包含 git 的原始输出。
	Pull(ctx context.Context, dir string) error
	// Head 返回 dir 的当前提交，用于判断一次 update 带来了哪些改动。
	Head(ctx context.Context, dir string) (string, error)
	// ChangedPaths 返回 from..to 之间改动的文件路径，相对仓库根。
	ChangedPaths(ctx context.Context, dir, from, to string) ([]string, error)
}

// ExecRunner 是 Runner 的真实实现。
type ExecRunner struct {
	// Stdout 与 Stderr 接收 git 的输出，让用户能看到进度。为 nil 时丢弃。
	Stdout io.Writer
	Stderr io.Writer
}

// Check 确认 git 可用。
func (r *ExecRunner) Check(ctx context.Context) error {
	if _, err := exec.LookPath("git"); err != nil {
		return errors.New("找不到 git 命令，请先安装 git")
	}
	return nil
}

// Clone 拉取仓库。浅克隆让体积接近 tarball，同时保留 git 元信息，
// 使 update 能用 git pull 而非重新下载整包。
func (r *ExecRunner) Clone(ctx context.Context, url, dest string, depth int) error {
	args := []string{"clone"}
	if depth > 0 {
		args = append(args, "--depth", fmt.Sprint(depth))
	}
	args = append(args, url, dest)
	return r.stream(ctx, "", args...)
}

// Pull 在 dir 里执行 git pull。
func (r *ExecRunner) Pull(ctx context.Context, dir string) error {
	// 浅克隆的仓库可以直接 pull，git 会继续按浅历史拉取。
	return r.stream(ctx, dir, "pull")
}

// Head 返回 dir 的当前提交。
func (r *ExecRunner) Head(ctx context.Context, dir string) (string, error) {
	out, err := r.capture(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// ChangedPaths 返回 from..to 之间改动的文件路径。
func (r *ExecRunner) ChangedPaths(ctx context.Context, dir, from, to string) ([]string, error) {
	if from == "" || to == "" || from == to {
		return nil, nil
	}
	out, err := r.capture(ctx, dir, "diff", "--name-only", from, to)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			paths = append(paths, line)
		}
	}
	return paths, nil
}

func (r *ExecRunner) capture(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

func (r *ExecRunner) stream(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stderr strings.Builder
	cmd.Stdout = r.Stdout
	if r.Stderr != nil {
		cmd.Stderr = io.MultiWriter(r.Stderr, &stderr)
	} else {
		cmd.Stderr = &stderr
	}
	if err := cmd.Run(); err != nil {
		// 把 git 的原始输出原样带出去：工具不替用户做合并决策，
		// 用户需要看到 git 自己说了什么才能判断。
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			return fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		return fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, detail)
	}
	return nil
}
