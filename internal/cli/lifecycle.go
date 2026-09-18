package cli

import (
	"bufio"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/polite-007/vulhub-cli/internal/compose"
)

// cmdUp 启动一个或多个靶场。
func (a *app) cmdUp(args []string) int {
	positional, err := parseFlags(args, nil)
	if err != nil {
		return a.usageError(err)
	}
	if err := requireTarget(positional); err != nil {
		return a.usageError(errors.New("用法：vulhub up <靶场>[,…]"))
	}
	targets, err := a.resolveAll(positional)
	if err != nil {
		return a.fail(err)
	}
	if err := a.deps.Compose.Check(a.ctx); err != nil {
		return a.fail(err)
	}

	for _, t := range targets {
		fmt.Fprintf(a.out, "正在启动 %d  %s\n", t.Number, t.Path)
		// 不做端口冲突预检：冲突由 Docker 自己报错。
		if err := a.deps.Compose.Up(a.ctx, a.absDir(t.Path)); err != nil {
			return a.fail(err)
		}
		a.printAccessURLs(t)
	}
	return ExitOK
}

// printAccessURLs 打印靶场全部映射到宿主机的端口。
//
// 多端口环境里替用户挑一个"主端口"是在猜，猜错了他还得自己去查，
// 所以全部打印出来，多两行而已。
//
// 输出不带协议前缀：靶场里既有 web 服务也有数据库，给 3306 标上 http://
// 是一句假话，而 主机:端口 本身已经足够可用。
func (a *app) printAccessURLs(t target) {
	containers, err := a.deps.Compose.Containers(a.ctx, a.absDir(t.Path))
	if err != nil {
		return
	}

	host, ipErr := a.deps.Host.LocalIP()
	if ipErr != nil || host == "" {
		// 拿不到本机地址时退回 vulhub 文档里的占位符，
		// 宁可给一个明显需要替换的值，也不要给一个错的 IP。
		host = "your-ip"
	}

	var lines []string
	for _, c := range containers {
		for _, p := range c.Ports {
			lines = append(lines, fmt.Sprintf("  %s:%d  (%s 服务，容器端口 %d/%s)",
				host, p.HostPort, c.Service, p.ContainerPort, p.Protocol))
		}
	}
	if len(lines) == 0 {
		return
	}
	fmt.Fprintln(a.out, "访问地址：")
	for _, line := range lines {
		fmt.Fprintln(a.out, line)
	}
}

// cmdStop 停止一个或多个靶场，容器与网络保留。
func (a *app) cmdStop(args []string) int {
	positional, err := parseFlags(args, nil)
	if err != nil {
		return a.usageError(err)
	}
	if err := requireTarget(positional); err != nil {
		return a.usageError(errors.New("用法：vulhub stop <靶场>[,…]"))
	}
	targets, err := a.resolveAll(positional)
	if err != nil {
		return a.fail(err)
	}
	if err := a.deps.Compose.Check(a.ctx); err != nil {
		return a.fail(err)
	}

	for _, t := range targets {
		if err := a.deps.Compose.Stop(a.ctx, a.absDir(t.Path)); err != nil {
			return a.fail(err)
		}
		fmt.Fprintf(a.out, "已停止 %d  %s\n", t.Number, t.Path)
	}
	return ExitOK
}

// cmdDel 销毁一个靶场。
//
// 它是唯一不可逆的操作，因此不接受多选，且在动手前要求二次确认。
func (a *app) cmdDel(args []string) int {
	var removeAll, assumeYes bool
	positional, err := parseFlags(args, map[string]*bool{
		"-a": &removeAll, "--all": &removeAll,
		"-y": &assumeYes, "--yes": &assumeYes,
	})
	if err != nil {
		return a.usageError(err)
	}
	if len(positional) != 1 {
		return a.usageError(errors.New("用法：vulhub del <靶场> [-a] [-y]"))
	}

	targets, err := a.resolveAll(positional)
	if err != nil {
		return a.fail(err)
	}
	// 逗号列表在参数校验那里仍是一个参数（"1,2"），必须在这里再拦一次：
	// 否则 del 会静默地只销毁第一个、把其余的丢掉。
	if len(targets) != 1 {
		return a.usageError(errors.New("del 一次只能销毁一个靶场，请分开执行"))
	}
	t := targets[0]

	// 无法确认该靶场在跑什么时不要继续。这是不可逆操作，
	// 打印"没有运行中的容器"而实际有东西在跑，比直接失败危险得多。
	containers, err := a.deps.Compose.Containers(a.ctx, a.absDir(t.Path))
	if err != nil {
		return a.errorf("无法确认该靶场的运行状态，已中止：%v", err)
	}

	// 即便确认被跳过（-y），也先把将要发生的事说清楚。
	fmt.Fprintf(a.out, "将要销毁：\n  编号 %d  靶场 %s\n", t.Number, t.Path)
	fmt.Fprintf(a.out, "  compose 项目：%s（其网络会一并删除）\n", a.projectName(t, containers))
	if len(containers) > 0 {
		fmt.Fprintln(a.out, "  容器：")
		for _, c := range containers {
			fmt.Fprintf(a.out, "    %s\n", c.Name)
		}
	} else {
		fmt.Fprintln(a.out, "  容器：（当前没有运行中的容器）")
	}
	if removeAll {
		fmt.Fprintln(a.out, "  volume：该靶场的全部数据卷")
		if images, err := a.deps.Compose.Images(a.ctx, a.absDir(t.Path)); err == nil && len(images) > 0 {
			fmt.Fprintln(a.out, "  镜像：")
			for _, img := range images {
				fmt.Fprintf(a.out, "    %s\n", img)
			}
		}
	}

	if !assumeYes && !a.confirm("确认销毁？输入 y 继续：") {
		fmt.Fprintln(a.out, "已取消")
		return ExitOK
	}

	var images []string
	if removeAll {
		// 只删该靶场引用的镜像，不做共享检查，也不做全局 prune：
		// 镜像被其他靶场共用时删掉的后果只是下次要重新拉取，不构成数据损失；
		// 而 prune 会波及 vulhub 之外属于用户自己的镜像。
		images, _ = a.deps.Compose.Images(a.ctx, a.absDir(t.Path))
	}

	if err := a.deps.Compose.Down(a.ctx, a.absDir(t.Path), removeAll); err != nil {
		return a.fail(err)
	}
	if len(images) > 0 {
		if err := a.deps.Compose.RemoveImages(a.ctx, images); err != nil {
			// 镜像被其他容器占用时 Docker 会拒绝删除，这是预期内的结果。
			fmt.Fprintf(a.errOut, "警告：部分镜像未能删除：%v\n", err)
		}
	}
	fmt.Fprintf(a.out, "已销毁 %d  %s\n", t.Number, t.Path)
	return ExitOK
}

// projectName 返回靶场对应的 compose 项目名。
//
// 优先取容器标签上的真实项目名；没有容器可查时退回目录名——
// compose 在靶场目录里执行时就是用目录名作项目名的。
func (a *app) projectName(t target, containers []compose.Container) string {
	for _, c := range containers {
		if c.Project != "" {
			return c.Project
		}
	}
	return filepath.Base(a.absDir(t.Path))
}

// confirm 在 out 上提示并读取一行输入，只有明确的肯定才算确认。
func (a *app) confirm(prompt string) bool {
	fmt.Fprint(a.out, prompt)
	reader := a.reader()
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

// reader 在输入流上建立唯一的缓冲读取器，避免多处缓冲互相吞字节。
func (a *app) reader() *bufio.Reader {
	if a.br == nil {
		a.br = bufio.NewReader(a.in)
	}
	return a.br
}
