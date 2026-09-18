// Package envindex 维护靶场路径与靶场编号之间的持久映射。
//
// 它是 vulhub-cli 唯一需要持久保存的状态。靶场路径是身份，编号只是别名：
// 索引丢失时编号会重新分配，但用户仍能用路径精确操作。
package envindex

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Index 是靶场路径到靶场编号的映射。
type Index struct {
	Numbers map[string]int `json:"numbers"`
}

// Load 读取索引文件。文件不存在时返回一个空索引而不是错误——首次运行属于正常情况。
func Load(path string) (*Index, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &Index{Numbers: map[string]int{}}, nil
		}
		return nil, fmt.Errorf("读取索引 %s: %w", path, err)
	}

	var idx Index
	if err := json.Unmarshal(raw, &idx); err != nil {
		return nil, fmt.Errorf("解析索引 %s: %w", path, err)
	}
	if idx.Numbers == nil {
		idx.Numbers = map[string]int{}
	}
	return &idx, nil
}

// Save 写入索引文件，必要时创建其所在目录。
func (i *Index) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建索引目录: %w", err)
	}
	raw, err := json.MarshalIndent(i, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化索引: %w", err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		return fmt.Errorf("写入索引 %s: %w", path, err)
	}
	return nil
}

// Ensure 为尚未分配编号的靶场路径依次分配编号，按传入顺序（调用方已排好字典序）。
// 它只新增、从不删除：消失的靶场其编号作废且不再复用，因此编号不会被重新分配给别人。
// 返回是否分配了新编号。
func (i *Index) Ensure(paths []string) bool {
	if i.Numbers == nil {
		i.Numbers = map[string]int{}
	}
	next := i.max() + 1
	changed := false
	for _, p := range paths {
		if _, ok := i.Numbers[p]; ok {
			continue
		}
		i.Numbers[p] = next
		next++
		changed = true
	}
	return changed
}

// Number 返回靶场路径对应的编号。
func (i *Index) Number(path string) (int, bool) {
	n, ok := i.Numbers[path]
	return n, ok
}

// PathByNumber 返回编号对应的靶场路径。
func (i *Index) PathByNumber(n int) (string, bool) {
	for path, num := range i.Numbers {
		if num == n {
			return path, true
		}
	}
	return "", false
}

// max 返回当前已分配的最大编号；索引为空时返回 0。
func (i *Index) max() int {
	max := 0
	for _, n := range i.Numbers {
		if n > max {
			max = n
		}
	}
	return max
}
