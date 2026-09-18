// Package catalog 负责产出一份完整的靶场清单。
//
// 它优先解析 vulhub 检出根目录的 environments.toml（vulhub 官方的环境注册表），
// 该文件缺失或解析失败时降级为扫描目录树——此时只能得到靶场路径，标题为空。
package catalog

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// registryFile 是 vulhub 官方的环境注册表文件名。
const registryFile = "environments.toml"

// composeFilenames 是靶场目录里被视为 compose 文件的候选名。
// vulhub 的规范要求用 docker-compose.yml，.yaml 只是容错。
var composeFilenames = []string{"docker-compose.yml", "docker-compose.yaml"}

// Environment 是 vulhub 中的一个靶场。
type Environment struct {
	// Path 是靶场路径，相对 vulhub 检出根目录，形如 "activemq/CVE-2023-46604"。
	Path string
	// Name 是漏洞标题。降级扫描目录树时为空。
	Name string
}

// Load 返回 vulhub 检出根目录下的全部靶场，按靶场路径字典序排列。
//
// 字典序是刻意的：它是靶场编号的分配依据，完全由本地目录决定，因此可复现。
func Load(root string) ([]Environment, error) {
	if envs, err := loadFromRegistry(root); err == nil {
		sort.Slice(envs, func(i, j int) bool { return envs[i].Path < envs[j].Path })
		return envs, nil
	}
	envs, err := scan(root)
	if err != nil {
		return nil, err
	}
	sort.Slice(envs, func(i, j int) bool { return envs[i].Path < envs[j].Path })
	return envs, nil
}

// loadFromRegistry 解析 environments.toml。任何失败都返回错误，由调用方降级。
func loadFromRegistry(root string) ([]Environment, error) {
	raw, err := os.ReadFile(filepath.Join(root, registryFile))
	if err != nil {
		return nil, err
	}

	var doc struct {
		Environment []struct {
			Name string `toml:"name"`
			Path string `toml:"path"`
		} `toml:"environment"`
	}
	if err := toml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("解析 %s: %w", registryFile, err)
	}
	if len(doc.Environment) == 0 {
		return nil, fmt.Errorf("%s 中没有任何 environment 条目", registryFile)
	}

	envs := make([]Environment, 0, len(doc.Environment))
	for _, e := range doc.Environment {
		path := strings.TrimSpace(e.Path)
		if path == "" {
			continue
		}
		envs = append(envs, Environment{Path: filepath.ToSlash(path), Name: strings.TrimSpace(e.Name)})
	}
	if len(envs) == 0 {
		return nil, fmt.Errorf("%s 中没有任何有效的 path 字段", registryFile)
	}
	return envs, nil
}

// scan 扫描目录树，找出所有含 compose 文件的目录。
//
// 跳过 .git（vulhub 检出本身就是 git 仓库）、base（只放基础镜像的 Dockerfile）
// 与 tests（上游的校验脚本）。
func scan(root string) ([]Environment, error) {
	if _, err := os.Stat(root); err != nil {
		return nil, fmt.Errorf("读取 vulhub 检出 %s: %w", root, err)
	}

	var envs []Environment
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if path != root {
			name := d.Name()
			if name == ".git" || name == "base" || name == "tests" {
				return fs.SkipDir
			}
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return nil
		}
		if hasComposeFile(path) {
			envs = append(envs, Environment{Path: filepath.ToSlash(rel)})
			return fs.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("扫描 vulhub 检出 %s: %w", root, err)
	}
	return envs, nil
}

func hasComposeFile(dir string) bool {
	for _, name := range composeFilenames {
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}
