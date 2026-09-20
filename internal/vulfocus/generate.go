package vulfocus

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	hubAPI   = "https://hub.docker.com/v2/repositories"
	hubLogin = "https://hub.docker.com/v2/users/login/"
	authAPI  = "https://auth.docker.io/token"
	registry = "https://registry-1.docker.io/v2"

	userAgent    = "vulhub-cli-gen-vulfocus"
	maxAttempts  = 5
	pageSize     = 100
	manifestAddr = "application/vnd.docker.distribution.manifest.v2+json, " +
		"application/vnd.oci.image.manifest.v1+json, " +
		"application/vnd.docker.distribution.manifest.list.v2+json, " +
		"application/vnd.oci.image.index.v1+json"
)

// Generator 从 Docker Hub 生成镜像清单。
//
// 它只被 `vulhub gen-vulfocus` 使用，用户侧的 init 与 update 都不会碰网络。
type Generator struct {
	// Client 为 nil 时使用一个带超时的默认客户端。
	Client *http.Client
	// OnProgress 每处理完一个镜像回调一次，便于报告进度。
	OnProgress func(done, total int, image string)

	// Username 与 Password 用于解除匿名分页上限。
	//
	// Docker Hub 对匿名请求的翻页限制是 offset 100，而 vulfocus 命名空间有
	// 452 个镜像——不登录就只拿得到前 100 个。**Password 必须是 Personal
	// Access Token，不是账户密码。**两者为空时走匿名访问。
	Username string
	Password string

	jwt string // 登录后换到的令牌，缓存起来
}

func (g *Generator) client() *http.Client {
	if g.Client != nil {
		return g.Client
	}
	return &http.Client{Timeout: 60 * time.Second}
}

// Skip 记录一个无法用作靶场的镜像。
type Skip struct {
	Image  string
	Reason string
}

// workers 是并发拉取镜像信息的协程数。
//
// 每个镜像要三次请求，四百多个镜像串行跑要四十多分钟。并发之后是几分钟。
// 取 6 是保守值：Docker Hub 对未认证的 registry 请求有速率限制，
// 而重试退避已经能兜住偶发的 429。
const workers = 6

// Generate 返回镜像清单，以及无法使用的镜像。
//
// 单个镜像不可用（最常见的是没有 latest 标签，`docker pull` 也拉不动它）
// 不会拖垮整轮生成，但**绝不静默**：调用方必须把 skipped 报给用户。
func (g *Generator) Generate(ctx context.Context) ([]Image, []Skip, error) {
	repos, err := g.List(ctx)
	if err != nil {
		return nil, nil, err
	}

	type outcome struct {
		image Image
		skip  *Skip
	}
	outcomes := make([]outcome, len(repos))

	var (
		wg     sync.WaitGroup
		sem    = make(chan struct{}, workers)
		done   atomic.Int64
		report sync.Mutex
	)
	for i, repo := range repos {
		wg.Add(1)
		go func(i int, repo Repo) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			ports, err := g.Ports(ctx, repo.Name)
			if err != nil {
				outcomes[i] = outcome{skip: &Skip{Image: repo.Name, Reason: err.Error()}}
			} else {
				outcomes[i] = outcome{image: Image{Image: repo.Name, Ports: ports, CreatedAt: repo.CreatedAt}}
			}

			if g.OnProgress != nil {
				// 进度回调是共享的输出流，必须串行化，否则几路会互相插字。
				report.Lock()
				defer report.Unlock()
				g.OnProgress(int(done.Add(1)), len(repos), repo.Name)
			}
		}(i, repo)
	}
	wg.Wait()

	images := make([]Image, 0, len(repos))
	var skipped []Skip
	for _, o := range outcomes {
		if o.skip != nil {
			skipped = append(skipped, *o.skip)
		} else {
			images = append(images, o.image)
		}
	}

	// 跳过比例过高说明不是"个别镜像坏了"，而是系统性问题——限流、网络故障，
	// 或者根本没带凭证。那种情况下写出的是**一份悄悄少了一百多个靶场的清单**，
	// 比直接失败难发现得多，所以这里必须判定整轮失败。
	if maxSkipped := len(repos) / 10; len(skipped) > maxSkipped {
		return nil, skipped, fmt.Errorf(
			"有 %d/%d 个镜像取不到，比例过高，多半是系统性原因（Docker Hub 限流是最常见的）；不发半份清单",
			len(skipped), len(repos))
	}
	sort.Slice(images, func(i, j int) bool { return images[i].Image < images[j].Image })
	sort.Slice(skipped, func(i, j int) bool { return skipped[i].Image < skipped[j].Image })

	if len(images) == 0 {
		return nil, skipped, errors.New("没有取到任何可用的镜像")
	}
	return images, skipped, nil
}

// Repo 是命名空间下的一个仓库。
type Repo struct {
	Name string
	// CreatedAt 是镜像的推送时间，取自 Docker Hub 的 last_updated。
	// 注意不是 last_modified——后者是所有仓库共享的元数据更新时间，
	// 用它排序会让所有镜像挤在同一个时间点上。
	CreatedAt time.Time
}

// List 返回命名空间下的全部仓库。
func (g *Generator) List(ctx context.Context) ([]Repo, error) {
	headers, err := g.hubHeaders(ctx)
	if err != nil {
		return nil, err
	}

	var repos []Repo
	var total int
	for page := 1; ; page++ {
		var resp struct {
			Next    string `json:"next"`
			Count   int    `json:"count"`
			Results []struct {
				Name        string `json:"name"`
				LastUpdated string `json:"last_updated"`
			} `json:"results"`
		}
		url := fmt.Sprintf("%s/%s/?page_size=%d&page=%d", hubAPI, namespace, pageSize, page)
		if err := g.get(ctx, url, headers, &resp); err != nil {
			if page > 1 && len(repos) < total {
				// 匿名访问会在这里撞上 offset 100 的上限。与其发出半份清单，
				// 不如直接失败——用户不会知道自己少了哪些靶场。
				return nil, fmt.Errorf(
					"只取到 %d/%d 个镜像就翻不动了。Docker Hub 对匿名请求限制翻页，请用 --username 与 --token 提供凭证：%w",
					len(repos), total, err)
			}
			return nil, err
		}
		total = resp.Count
		for _, r := range resp.Results {
			// 命名空间下有一个与命名空间同名的仓库，那是平台本体，不是靶场。
			if r.Name == namespace {
				continue
			}
			repos = append(repos, Repo{Name: r.Name, CreatedAt: parsePushedAt(r.LastUpdated)})
		}
		if resp.Next == "" {
			break
		}
	}
	sort.Slice(repos, func(i, j int) bool { return repos[i].Name < repos[j].Name })
	return repos, nil
}

// parsePushedAt 解析 Docker Hub 的时间戳。解析不了就返回零值——
// 一个没有时间的靶场会排到列表末尾，而不是让整轮生成失败。
func parsePushedAt(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// hubHeaders 返回访问 Hub API 所需的请求头，未配置凭证时为空。
func (g *Generator) hubHeaders(ctx context.Context) (map[string]string, error) {
	token, err := g.authToken(ctx)
	if err != nil {
		return nil, err
	}
	if token == "" {
		return nil, nil
	}
	return map[string]string{"Authorization": "JWT " + token}, nil
}

// authToken 用凭证换取 Hub API 的令牌，并把结果缓存下来。匿名时返回空串。
func (g *Generator) authToken(ctx context.Context) (string, error) {
	if g.Username == "" || g.Password == "" {
		return "", nil
	}
	if g.jwt != "" {
		return g.jwt, nil
	}

	payload, err := json.Marshal(map[string]string{
		"username": g.Username,
		"password": g.Password,
	})
	if err != nil {
		return "", err
	}
	body, err := g.request(ctx, http.MethodPost, hubLogin,
		map[string]string{"Content-Type": "application/json"}, payload)
	if err != nil {
		return "", fmt.Errorf("登录 Docker Hub 失败（请确认 --token 用的是 Personal Access Token，不是账户密码）: %w", err)
	}

	var resp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("解析 Docker Hub 登录响应: %w", err)
	}
	if resp.Token == "" {
		return "", errors.New("Docker Hub 登录成功但没有返回令牌")
	}
	g.jwt = resp.Token
	return g.jwt, nil
}

// Ports 返回一个镜像声明的 TCP 端口，升序。
//
// 只读 manifest 与 config blob（约 12KB），不拉取镜像本身。
func (g *Generator) Ports(ctx context.Context, image string) ([]int, error) {
	repo := namespace + "/" + image

	// registry 的令牌请求也要带凭证。匿名访问有速率限制，实测四百多个镜像
	// 跑到一半就会撞上 429——而那不是"这个镜像坏了"，是整轮都拿不全。
	headers := map[string]string{}
	if g.Username != "" && g.Password != "" {
		headers["Authorization"] = "Basic " +
			base64.StdEncoding.EncodeToString([]byte(g.Username+":"+g.Password))
	}

	var tok struct {
		Token string `json:"token"`
	}
	authURL := fmt.Sprintf("%s?service=registry.docker.io&scope=repository:%s:pull", authAPI, repo)
	if err := g.get(ctx, authURL, headers, &tok); err != nil {
		return nil, err
	}
	auth := map[string]string{"Authorization": "Bearer " + tok.Token}

	digest, err := g.configDigest(ctx, repo, "latest", auth)
	if err != nil {
		// 我们合成的 compose 文件不写标签，也就是用 latest。
		// 没有 latest 的镜像，`docker pull vulfocus/<名字>` 同样拉不动，
		// 因此它不能算作一个可用的靶场。
		return nil, fmt.Errorf("没有 latest 标签，无法作为靶场使用: %w", err)
	}

	var cfg struct {
		Config struct {
			ExposedPorts map[string]struct{} `json:"ExposedPorts"`
		} `json:"config"`
	}
	if err := g.get(ctx, fmt.Sprintf("%s/%s/blobs/%s", registry, repo, digest), auth, &cfg); err != nil {
		return nil, err
	}
	return tcpPorts(cfg.Config.ExposedPorts), nil
}

// configDigest 返回镜像 config blob 的摘要。
// reference 是 tag 或 digest；若指向的是多架构清单，则挑出 linux/amd64 那一份。
func (g *Generator) configDigest(ctx context.Context, repo, reference string, auth map[string]string) (string, error) {
	var manifest struct {
		Config struct {
			Digest string `json:"digest"`
		} `json:"config"`
		Manifests []struct {
			Digest   string `json:"digest"`
			Platform struct {
				OS           string `json:"os"`
				Architecture string `json:"architecture"`
			} `json:"platform"`
		} `json:"manifests"`
	}
	url := fmt.Sprintf("%s/%s/manifests/%s", registry, repo, reference)
	if err := g.get(ctx, url, withAccept(auth, manifestAddr), &manifest); err != nil {
		return "", err
	}
	if manifest.Config.Digest != "" {
		return manifest.Config.Digest, nil
	}

	// 多架构清单：挑 linux/amd64。挑不到就退回第一个，总比直接失败有用。
	pick := ""
	for _, m := range manifest.Manifests {
		if m.Platform.OS == "linux" && m.Platform.Architecture == "amd64" {
			pick = m.Digest
			break
		}
	}
	if pick == "" && len(manifest.Manifests) > 0 {
		pick = manifest.Manifests[0].Digest
	}
	if pick == "" {
		return "", fmt.Errorf("%s:%s 的清单里既没有 config 也没有子清单", repo, reference)
	}
	return g.configDigest(ctx, repo, pick, auth)
}

// tcpPorts 把 "80/tcp" 形式的键摊平成端口号，丢掉非 TCP 的。
// 没有可用的端口时返回空切片而不是 nil，免得序列化成 JSON 的 null。
func tcpPorts(exposed map[string]struct{}) []int {
	ports := []int{}
	for key := range exposed {
		port, protocol, found := strings.Cut(key, "/")
		if found && protocol != "tcp" {
			continue
		}
		n, err := strconv.Atoi(port)
		if err != nil || n <= 0 {
			continue
		}
		ports = append(ports, n)
	}
	sort.Ints(ports)
	return ports
}

func withAccept(h map[string]string, accept string) map[string]string {
	out := make(map[string]string, len(h)+1)
	for k, v := range h {
		out[k] = v
	}
	out["Accept"] = accept
	return out
}

// get 发起一次 GET 并解析 JSON。
func (g *Generator) get(ctx context.Context, url string, headers map[string]string, out any) error {
	body, err := g.request(ctx, http.MethodGet, url, headers, nil)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%s: 解析响应: %w", url, err)
	}
	return nil
}

// request 发起一次请求，对网络故障与限流做退避重试。
func (g *Generator) request(ctx context.Context, method, url string, headers map[string]string, payload []byte) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		body, retryable, err := g.doOnce(ctx, method, url, headers, payload)
		if err == nil {
			return body, nil
		}
		if !retryable {
			return nil, err
		}
		lastErr = err
	}
	return nil, fmt.Errorf("重试 %d 次仍失败: %w", maxAttempts, lastErr)
}

// doOnce 发起一次请求，并指出失败是否值得重试。
//
// 401 与 404 不重试：凭证不对或资源不存在，重试多少次都一样。
// 403 与 429 要重试：Docker Hub 用它们表达限流。
func (g *Generator) doOnce(ctx context.Context, method, url string, headers map[string]string, payload []byte) (body []byte, retryable bool, err error) {
	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", userAgent)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := g.client().Do(req)
	if err != nil {
		return nil, true, err
	}
	defer resp.Body.Close()

	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	detail := strings.TrimSpace(string(body))
	if len(detail) > 300 {
		detail = detail[:300]
	}

	switch {
	case resp.StatusCode == http.StatusOK:
		return body, false, nil
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusNotFound:
		return nil, false, fmt.Errorf("HTTP %d: %s", resp.StatusCode, detail)
	case resp.StatusCode == http.StatusForbidden, resp.StatusCode == http.StatusTooManyRequests, resp.StatusCode >= 500:
		return nil, true, fmt.Errorf("HTTP %d: %s", resp.StatusCode, detail)
	default:
		return nil, false, fmt.Errorf("HTTP %d: %s", resp.StatusCode, detail)
	}
}
