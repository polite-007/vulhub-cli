package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/polite-007/vulhub-cli/internal/vulfocus"
)

// ensureEnvironment 确保靶场的 compose 文件存在，并返回其目录。
//
// vulhub 来源的靶场目录由检出提供。vulfocus 来源的靶场没有目录——上游只有
// 镜像——所以它的 compose 文件在这里按需合成。合成文件是纯派生数据，
// 随时可以从内嵌的镜像清单重新生成，丢失无碍。
func (a *app) ensureEnvironment(path string) (string, error) {
	if !vulfocus.IsVulfocus(path) {
		return a.absDir(path), nil
	}

	dir := a.absDir(path)
	composeFile := filepath.Join(dir, "docker-compose.yml")
	if _, err := os.Stat(composeFile); err == nil {
		return dir, nil
	}

	image, ok := a.vulfocusImage(vulfocus.ImageName(path))
	if !ok {
		return "", fmt.Errorf("镜像清单里没有 %s", path)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建 %s: %w", dir, err)
	}
	if err := os.WriteFile(composeFile, []byte(renderCompose(image)), 0o644); err != nil {
		return "", fmt.Errorf("写入 %s: %w", composeFile, err)
	}
	return dir, nil
}

// vulfocusImage 在当前生效的清单里查找一个镜像。
func (a *app) vulfocusImage(image string) (vulfocus.Image, bool) {
	images, err := a.vulfocusImages()
	if err != nil {
		return vulfocus.Image{}, false
	}
	return vulfocus.FindIn(images, image)
}

// renderCompose 为一个 vulfocus 镜像合成 compose 文件。//
// 镜像自带 Entrypoint/Cmd，会自己把服务起起来，所以不需要 command。
// 端口全部发布，宿主端口与容器端口相同——多发布几个非 web 端口是无害的，
// up 本来就会把所有映射端口打出来。
func renderCompose(img vulfocus.Image) string {
	var b strings.Builder
	b.WriteString("services:\n")
	b.WriteString("  target:\n")
	fmt.Fprintf(&b, "    image: %s\n", vulfocus.Reference(img.Image))
	if len(img.Ports) > 0 {
		b.WriteString("    ports:\n")
		for _, p := range img.Ports {
			fmt.Fprintf(&b, "      - \"%d:%d\"\n", p, p)
		}
	}
	return b.String()
}
