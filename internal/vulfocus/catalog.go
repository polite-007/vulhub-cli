// Package vulfocus 提供 vulfocus 来源的靶场。
//
// vulfocus 上游（Docker Hub 的 vulfocus 命名空间）只提供镜像，没有 compose
// 文件、也没有任何启动说明。本包负责两件事：
//
//   - 提供一份**内嵌**的镜像清单（镜像名 + 端口），用户在 init 时零网络即可拿到
//   - 从镜像名推导出可读的漏洞标题，让 ls 与 search 可用
//
// 清单由 `vulhub gen-vulfocus` 生成，随二进制分发。取舍见
// docs/adr/0002-embedded-vulfocus-catalog.md。
package vulfocus

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// prefix 是 vulfocus 靶场在靶场路径上的前缀。它同时充当"来源"的判据。
const prefix = "vulfocus/"

//go:embed data/vulfocus.json
var catalogJSON []byte

// Image 是镜像清单中的一项。
type Image struct {
	// Image 是镜像名，不含命名空间，例如 "drupal-cve_2018_7600"。
	Image string `json:"image"`
	// Ports 是镜像声明的端口。它们全部会被映射到宿主，且宿主端口与容器端口相同。
	Ports []int `json:"ports"`
	// CreatedAt 是镜像的推送时间，用于 ls -time。零值表示清单里没有记录。
	CreatedAt time.Time `json:"created_at,omitzero"`
}

// Environment 是清单中的一个靶场。
type Environment struct {
	// Path 是靶场路径，形如 "vulfocus/drupal-cve_2018_7600"。
	Path string
	// Title 是从镜像名推导出的漏洞标题。
	Title string
	// CreatedAt 是镜像的推送时间，零值表示未知。
	CreatedAt time.Time
}

// Images 返回内嵌的镜像清单，按镜像名排序。
func Images() ([]Image, error) {
	var images []Image
	if err := json.Unmarshal(catalogJSON, &images); err != nil {
		return nil, fmt.Errorf("解析内嵌的 vulfocus 镜像清单: %w", err)
	}
	sort.Slice(images, func(i, j int) bool { return images[i].Image < images[j].Image })
	return images, nil
}

// Environments 返回内嵌清单的全部靶场。
func Environments() ([]Environment, error) {
	images, err := Images()
	if err != nil {
		return nil, err
	}
	return EnvironmentsOf(images), nil
}

// EnvironmentsOf 把一份镜像清单转成靶场清单。
func EnvironmentsOf(images []Image) []Environment {
	out := make([]Environment, 0, len(images))
	for _, img := range images {
		out = append(out, Environment{
			Path:      prefix + img.Image,
			Title:     Title(img.Image),
			CreatedAt: img.CreatedAt,
		})
	}
	return out
}

// IsVulfocus 判断一个靶场路径是否属于 vulfocus 来源。
func IsVulfocus(path string) bool {
	return strings.HasPrefix(path, prefix)
}

// ImageName 返回 vulfocus 靶场路径对应的镜像名。
func ImageName(path string) string {
	return strings.TrimPrefix(path, prefix)
}

// Reference 返回镜像的完整引用，含命名空间。
func Reference(image string) string {
	return namespace + "/" + image
}

// Find 按镜像名在内嵌清单里查找。
func Find(image string) (Image, bool) {
	images, err := Images()
	if err != nil {
		return Image{}, false
	}
	return FindIn(images, image)
}

// FindIn 按镜像名在给定清单里查找。
func FindIn(images []Image, image string) (Image, bool) {
	for _, img := range images {
		if img.Image == image {
			return img, true
		}
	}
	return Image{}, false
}

// namespace 是 vulfocus 镜像所在的 Docker Hub 命名空间。
const namespace = "vulfocus"

// vulnID 匹配镜像名里表示漏洞编号的那一段。
//
// 两种写法都要认：drupal-cve_2018_7600 与 log4j2-cve-2021-4104。
// 早期实现先按 "-" 切分镜像名再逐段匹配，于是连字符写法的编号被切成三段、
// 永远匹配不上，标题退化成原样的镜像名——而那正好打掉了标题推导存在的理由：
// 用户按 "CVE-2021-4104" 搜不到 log4j2-cve-2021-4104。
var vulnID = regexp.MustCompile(`(?i)(cve|cnvd|wooyun)[-_](\d{4})[-_](\d+)`)

// Title 从镜像名推导漏洞标题：
//
//	drupal-cve_2018_7600      → "Drupal CVE-2018-7600"
//	log4j2-cve-2021-4104      → "Log4j2 CVE-2021-4104"
//	vtiger-cve_2020_19363-fix → "Vtiger CVE-2020-19363 (fixed)"
//
// Docker Hub 的 description 字段基本是空的，官方平台的"漏洞名称"也够不到，
// 所以只能从名字本身推导。这不是猜测——纯字符串变换，没有引入任何判断。
// 它的真正价值在于让**按 CVE 号检索**变得可用。
func Title(image string) string {
	m := vulnID.FindStringSubmatchIndex(image)
	if m == nil {
		// 名字里没有可识别的漏洞编号，原样返回，不做无根据的加工。
		return image
	}

	id := fmt.Sprintf("%s-%s-%s",
		strings.ToUpper(image[m[2]:m[3]]), image[m[4]:m[5]], image[m[6]:m[7]])
	software := titleWords(strings.Trim(image[:m[0]], "-_"))

	// "-fix" 后缀表示这是该漏洞的**已修复**版本，不是一个可打的靶场。
	// 不加标注的话它和漏洞版的标题会一模一样，列表里根本分不出来。
	suffix := ""
	if strings.HasPrefix(image[m[1]:], "-fix") {
		suffix = " (fixed)"
	}

	if software == "" {
		return id + suffix
	}
	return software + " " + id + suffix
}

// titleWords 把 kebab-case 的软件名转成词首大写："apache-druid" → "Apache Druid"。
func titleWords(s string) string {
	if s == "" {
		return ""
	}
	words := strings.Split(s, "-")
	for i, w := range words {
		if w == "" {
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}
