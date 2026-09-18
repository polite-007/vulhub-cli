package cli_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/polite-007/vulhub-cli/internal/cli"
	"github.com/polite-007/vulhub-cli/internal/compose"
	"github.com/polite-007/vulhub-cli/internal/gitx"
	"github.com/polite-007/vulhub-cli/internal/hostinfo"
)

// --- 假的外部依赖 ---
//
// 只有这三个依赖需要假对象：真实实现分别要跑起 Docker、联网拉取数百 MB、
// 以及返回值取决于运行环境。文件系统与 environments.toml 解析一律用真实实现
// （见 harness 里的临时目录）。

type downCall struct {
	Dir           string
	RemoveVolumes bool
}

type fakeCompose struct {
	checkErr     error
	upErr        error
	stopErr      error
	downErr      error
	pullErr      error
	imagesErr    error
	removeErr    error
	containerErr error

	images     map[string][]string
	containers []compose.Container

	upCalls      []string
	stopCalls    []string
	downCalls    []downCall
	pullCalls    []string
	removed      [][]string
	containerDir []string
}

func (f *fakeCompose) Check(context.Context) error { return f.checkErr }

func (f *fakeCompose) Up(_ context.Context, dir string) error {
	f.upCalls = append(f.upCalls, dir)
	return f.upErr
}

func (f *fakeCompose) Stop(_ context.Context, dir string) error {
	f.stopCalls = append(f.stopCalls, dir)
	return f.stopErr
}

func (f *fakeCompose) Down(_ context.Context, dir string, removeVolumes bool) error {
	f.downCalls = append(f.downCalls, downCall{Dir: dir, RemoveVolumes: removeVolumes})
	return f.downErr
}

func (f *fakeCompose) Pull(_ context.Context, dir string) error {
	f.pullCalls = append(f.pullCalls, dir)
	return f.pullErr
}

func (f *fakeCompose) Images(_ context.Context, dir string) ([]string, error) {
	if f.imagesErr != nil {
		return nil, f.imagesErr
	}
	return f.images[dir], nil
}

func (f *fakeCompose) RemoveImages(_ context.Context, images []string) error {
	f.removed = append(f.removed, images)
	return f.removeErr
}

func (f *fakeCompose) Containers(_ context.Context, workDir string) ([]compose.Container, error) {
	f.containerDir = append(f.containerDir, workDir)
	if f.containerErr != nil {
		return nil, f.containerErr
	}
	if workDir == "" {
		return f.containers, nil
	}
	var out []compose.Container
	for _, c := range f.containers {
		if c.WorkDir == workDir {
			out = append(out, c)
		}
	}
	return out, nil
}

type cloneCall struct {
	URL   string
	Dest  string
	Depth int
}

type fakeGit struct {
	checkErr   error
	cloneErr   error
	pullErr    error
	headErr    error
	changedErr error

	// cloneFn 让测试模拟"克隆真的把内容放到了目标目录"。
	cloneFn func(url, dest string) error

	heads   []string // 依次返回的 HEAD，用于模拟 pull 前后的差异
	changed []string

	cloneCalls []cloneCall
	pullCalls  []string
	headCalls  int
}

func (f *fakeGit) Check(context.Context) error { return f.checkErr }

func (f *fakeGit) Clone(_ context.Context, url, dest string, depth int) error {
	f.cloneCalls = append(f.cloneCalls, cloneCall{URL: url, Dest: dest, Depth: depth})
	if f.cloneFn != nil {
		return f.cloneFn(url, dest)
	}
	return f.cloneErr
}

func (f *fakeGit) Pull(_ context.Context, dir string) error {
	f.pullCalls = append(f.pullCalls, dir)
	return f.pullErr
}

func (f *fakeGit) Head(context.Context, string) (string, error) {
	if f.headErr != nil {
		return "", f.headErr
	}
	if f.headCalls < len(f.heads) {
		h := f.heads[f.headCalls]
		f.headCalls++
		return h, nil
	}
	return "head", nil
}

func (f *fakeGit) ChangedPaths(context.Context, string, string, string) ([]string, error) {
	return f.changed, f.changedErr
}

type fakeHost struct {
	ip  string
	err error
}

func (f fakeHost) LocalIP() (string, error) { return f.ip, f.err }

var _ compose.Runner = (*fakeCompose)(nil)
var _ gitx.Runner = (*fakeGit)(nil)
var _ hostinfo.Info = fakeHost{}

// --- 测试夹具 ---

// harness 用临时目录承载真实的文件系统与真实的 environments.toml 解析。
type harness struct {
	t       *testing.T
	root    string // vulhub 检出根目录
	index   string // 索引文件路径
	compose *fakeCompose
	git     *fakeGit
	host    fakeHost
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	home := t.TempDir()
	h := &harness{
		t:       t,
		root:    filepath.Join(home, "vulhub"),
		index:   filepath.Join(home, ".local", "share", "vulhub-cli", "index.json"),
		compose: &fakeCompose{images: map[string][]string{}},
		git:     &fakeGit{},
		host:    fakeHost{ip: "10.0.0.5"},
	}
	return h
}

// seed 在检出里铺出若干靶场，并写出 environments.toml。
// 传入的 envs 是 "路径=漏洞标题" 的形式。
func (h *harness) seed(envs ...string) {
	h.t.Helper()
	if err := os.MkdirAll(h.root, 0o755); err != nil {
		h.t.Fatal(err)
	}
	var registry strings.Builder
	for _, e := range envs {
		path, name, _ := strings.Cut(e, "=")
		dir := filepath.Join(h.root, filepath.FromSlash(path))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			h.t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("services: {}\n"), 0o644); err != nil {
			h.t.Fatal(err)
		}
		fmt.Fprintf(&registry, "[[environment]]\nname = %q\npath = %q\n\n", name, path)
	}
	if err := os.WriteFile(filepath.Join(h.root, "environments.toml"), []byte(registry.String()), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

// seedWithoutRegistry 铺出靶场目录但不写 environments.toml，用于走降级路径。
func (h *harness) seedWithoutRegistry(paths ...string) {
	h.t.Helper()
	for _, path := range paths {
		dir := filepath.Join(h.root, filepath.FromSlash(path))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			h.t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("services: {}\n"), 0o644); err != nil {
			h.t.Fatal(err)
		}
	}
}

func (h *harness) dir(path string) string {
	return filepath.Join(h.root, filepath.FromSlash(path))
}

// run 从唯一的测试缝进入：喂 args，断言输出流、依赖调用与退出码。
func (h *harness) run(args ...string) result {
	return h.runWith("", false, args...)
}

func (h *harness) runWith(stdin string, isTTY bool, args ...string) result {
	return h.runInput(strings.NewReader(stdin), isTTY, args...)
}

func (h *harness) runInput(in io.Reader, isTTY bool, args ...string) result {
	h.t.Helper()
	var out, errOut bytes.Buffer
	code := cli.Run(context.Background(), args, in, &out, &errOut, isTTY, h.deps())
	return result{code: code, out: out.String(), err: errOut.String()}
}

func (h *harness) deps() cli.Deps {
	return cli.Deps{
		Compose:    h.compose,
		Git:        h.git,
		Host:       h.host,
		VulhubRoot: h.root,
		IndexPath:  h.index,
		RepoURL:    "https://example.invalid/vulhub.git",
	}
}

type result struct {
	code int
	out  string
	err  string
}

func (r result) requireCode(t *testing.T, want int) result {
	t.Helper()
	if r.code != want {
		t.Fatalf("退出码 = %d, 期望 %d\nstdout:\n%s\nstderr:\n%s", r.code, want, r.out, r.err)
	}
	return r
}

func (r result) requireOut(t *testing.T, want string) result {
	t.Helper()
	if !strings.Contains(r.out, want) {
		t.Fatalf("stdout 未包含 %q\n实际 stdout:\n%s\nstderr:\n%s", want, r.out, r.err)
	}
	return r
}

func (r result) requireNoOut(t *testing.T, unwanted string) result {
	t.Helper()
	if strings.Contains(r.out, unwanted) {
		t.Fatalf("stdout 不应包含 %q\n实际 stdout:\n%s", unwanted, r.out)
	}
	return r
}

func (r result) requireErr(t *testing.T, want string) result {
	t.Helper()
	if !strings.Contains(r.err, want) {
		t.Fatalf("stderr 未包含 %q\n实际 stderr:\n%s", want, r.err)
	}
	return r
}

// indexFile 读回索引文件原文，用于断言编号的持久化行为。
func (h *harness) indexFile() string {
	h.t.Helper()
	raw, err := os.ReadFile(h.index)
	if err != nil {
		return ""
	}
	return string(raw)
}
