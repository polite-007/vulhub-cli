package cli_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/polite-007/vulhub-cli/internal/compose"
)

// --- up ---

func TestUpResolvesEnvironmentByPath(t *testing.T) {
	h := newHarness(t)
	h.seed("activemq/CVE-2023-46604=Apache ActiveMQ RCE")

	h.run("up", "activemq/CVE-2023-46604").requireCode(t, 0)

	if len(h.compose.upCalls) != 1 {
		t.Fatalf("期望 Up 被调用 1 次，实际 %d 次", len(h.compose.upCalls))
	}
	if want := h.dir("activemq/CVE-2023-46604"); h.compose.upCalls[0] != want {
		t.Fatalf("Up 收到的目录 = %q, 期望 %q", h.compose.upCalls[0], want)
	}
}

func TestUpResolvesEnvironmentByNumber(t *testing.T) {
	h := newHarness(t)
	h.seed(
		"activemq/CVE-2023-46604=Apache ActiveMQ RCE",
		"log4j/CVE-2021-44228=Log4j2 RCE",
	)

	// 编号按路径字典序分配：activemq=1，log4j=2。
	h.run("up", "2").requireCode(t, 0)

	if len(h.compose.upCalls) != 1 || h.compose.upCalls[0] != h.dir("log4j/CVE-2021-44228") {
		t.Fatalf("编号 2 未解析到 log4j：%v", h.compose.upCalls)
	}
}

func TestUpPrintsAccessURLsWithHostIPAndPorts(t *testing.T) {
	h := newHarness(t)
	h.seed("activemq/CVE-2023-46604=Apache ActiveMQ RCE")
	h.compose.containers = []compose.Container{{
		Name:    "activemq-cve-2023-46604-web-1",
		Service: "web",
		State:   "running",
		WorkDir: h.dir("activemq/CVE-2023-46604"),
		Ports:   []compose.PortMapping{{HostPort: 8161, ContainerPort: 8161, Protocol: "tcp"}},
	}}

	r := h.run("up", "activemq/CVE-2023-46604").requireCode(t, 0)
	r.requireOut(t, "10.0.0.5:8161")
	// 不猜协议：靶场里既有 web 服务也有数据库，给 3306 标 http:// 是一句假话。
	r.requireNoOut(t, "http://")
}

func TestUpPrintsEveryPublishedPortNotJustOne(t *testing.T) {
	h := newHarness(t)
	h.seed("joomla/CVE-2015-8562=Joomla RCE")
	h.compose.containers = []compose.Container{
		{
			Name: "web", Service: "web", WorkDir: h.dir("joomla/CVE-2015-8562"),
			Ports: []compose.PortMapping{{HostPort: 8080, ContainerPort: 80, Protocol: "tcp"}},
		},
		{
			Name: "mysql", Service: "mysql", WorkDir: h.dir("joomla/CVE-2015-8562"),
			Ports: []compose.PortMapping{{HostPort: 3306, ContainerPort: 3306, Protocol: "tcp"}},
		},
	}

	r := h.run("up", "joomla/CVE-2015-8562").requireCode(t, 0)
	r.requireOut(t, "10.0.0.5:8080")
	r.requireOut(t, "10.0.0.5:3306")
}

// 拿不到本机地址时退回占位符，而不是给出一个错的 IP。
func TestUpFallsBackToPlaceholderWhenHostIPIsUnavailable(t *testing.T) {
	h := newHarness(t)
	h.seed("activemq/CVE-2023-46604=Apache ActiveMQ RCE")
	h.host = fakeHost{err: errors.New("没有可用网卡")}
	h.compose.containers = []compose.Container{{
		Name: "web", Service: "web", WorkDir: h.dir("activemq/CVE-2023-46604"),
		Ports: []compose.PortMapping{{HostPort: 8161, ContainerPort: 8161, Protocol: "tcp"}},
	}}

	r := h.run("up", "activemq/CVE-2023-46604").requireCode(t, 0)
	r.requireOut(t, "your-ip:8161")
}

func TestUpAcceptsCommaSeparatedEnvironments(t *testing.T) {
	h := newHarness(t)
	h.seed(
		"activemq/CVE-2023-46604=Apache ActiveMQ RCE",
		"joomla/CVE-2015-8562=Joomla RCE",
		"log4j/CVE-2021-44228=Log4j2 RCE",
	)

	h.run("up", "1,3").requireCode(t, 0)

	if len(h.compose.upCalls) != 2 {
		t.Fatalf("期望 Up 被调用 2 次，实际 %d 次：%v", len(h.compose.upCalls), h.compose.upCalls)
	}
	if h.compose.upCalls[0] != h.dir("activemq/CVE-2023-46604") ||
		h.compose.upCalls[1] != h.dir("log4j/CVE-2021-44228") {
		t.Fatalf("多选靶场解析不符：%v", h.compose.upCalls)
	}
}

func TestUpWithoutArgumentsIsUsageError(t *testing.T) {
	h := newHarness(t)
	h.run("up").requireCode(t, 2)
}

func TestUpRejectsUnassignedNumber(t *testing.T) {
	h := newHarness(t)
	h.seed("activemq/CVE-2023-46604=Apache ActiveMQ RCE")
	h.run("up", "999").requireCode(t, 1).requireErr(t, "尚未分配")
}

func TestUpRejectsUnknownPath(t *testing.T) {
	h := newHarness(t)
	h.seed("activemq/CVE-2023-46604=Apache ActiveMQ RCE")
	h.run("up", "nope/CVE-0000-0000").requireCode(t, 1).requireErr(t, "找不到靶场")
}

// 编号指向的靶场被 update 删掉后，必须给出可操作的提示而不是含糊的失败。
func TestUpReportsStaleNumberWhoseEnvironmentIsGone(t *testing.T) {
	h := newHarness(t)
	h.seed("activemq/CVE-2023-46604=Apache ActiveMQ RCE")
	h.writeIndex(`{"numbers":{"gone/CVE-2000-0001":1}}`)

	h.run("up", "1").requireCode(t, 1).requireErr(t, "已不在 vulhub 检出中")
}

func TestUpSurfacesComposeFailure(t *testing.T) {
	h := newHarness(t)
	h.seed("activemq/CVE-2023-46604=Apache ActiveMQ RCE")
	h.compose.upErr = errors.New("端口已被占用")

	h.run("up", "1").requireCode(t, 1).requireErr(t, "端口已被占用")
}

func TestUpReportsMissingComposeBeforeTouchingAnything(t *testing.T) {
	h := newHarness(t)
	h.seed("activemq/CVE-2023-46604=Apache ActiveMQ RCE")
	h.compose.checkErr = errors.New("找不到 docker 命令，请先安装 Docker")

	h.run("up", "1").requireCode(t, 1).requireErr(t, "找不到 docker 命令")
	if len(h.compose.upCalls) != 0 {
		t.Fatalf("前置检查失败时不应调用 Up：%v", h.compose.upCalls)
	}
}

// --- stop ---

func TestStopAcceptsCommaSeparatedEnvironments(t *testing.T) {
	h := newHarness(t)
	h.seed(
		"activemq/CVE-2023-46604=Apache ActiveMQ RCE",
		"log4j/CVE-2021-44228=Log4j2 RCE",
	)

	h.run("stop", "1,2").requireCode(t, 0)
	if len(h.compose.stopCalls) != 2 {
		t.Fatalf("期望 Stop 被调用 2 次，实际 %d 次", len(h.compose.stopCalls))
	}
}

func TestStopWithoutArgumentsIsUsageError(t *testing.T) {
	h := newHarness(t)
	h.run("stop").requireCode(t, 2)
}

// --- del ---

func TestDelAsksForConfirmationAndDoesNothingWithoutAnAnswer(t *testing.T) {
	h := newHarness(t)
	h.seed("activemq/CVE-2023-46604=Apache ActiveMQ RCE")

	r := h.run("del", "1").requireCode(t, 0).requireOut(t, "确认销毁")
	r.requireOut(t, "已取消")
	if len(h.compose.downCalls) != 0 {
		t.Fatalf("未确认时不应销毁：%v", h.compose.downCalls)
	}
}

func TestDelProceedsWhenConfirmed(t *testing.T) {
	h := newHarness(t)
	h.seed("activemq/CVE-2023-46604=Apache ActiveMQ RCE")

	h.runWith("y\n", false, "del", "1").requireCode(t, 0).requireOut(t, "已销毁")

	if len(h.compose.downCalls) != 1 {
		t.Fatalf("期望 Down 被调用 1 次，实际 %d 次", len(h.compose.downCalls))
	}
	if h.compose.downCalls[0].RemoveVolumes {
		t.Fatal("默认不应删除 volume")
	}
}

func TestDelTreatsAnythingOtherThanYesAsCancellation(t *testing.T) {
	h := newHarness(t)
	h.seed("activemq/CVE-2023-46604=Apache ActiveMQ RCE")

	h.runWith("n\n", false, "del", "1").requireCode(t, 0).requireOut(t, "已取消")
	if len(h.compose.downCalls) != 0 {
		t.Fatalf("回答 n 时不应销毁：%v", h.compose.downCalls)
	}
}

func TestDelSkipsConfirmationWithYesFlag(t *testing.T) {
	h := newHarness(t)
	h.seed("activemq/CVE-2023-46604=Apache ActiveMQ RCE")

	r := h.run("del", "-y", "1").requireCode(t, 0)
	r.requireNoOut(t, "确认销毁")
	if len(h.compose.downCalls) != 1 {
		t.Fatalf("期望 Down 被调用 1 次，实际 %d 次", len(h.compose.downCalls))
	}
}

// -a 是 "all"：连 volume 与该靶场引用的镜像一起删。
func TestDelAllAlsoRemovesVolumesAndImages(t *testing.T) {
	h := newHarness(t)
	h.seed("activemq/CVE-2023-46604=Apache ActiveMQ RCE")
	dir := h.dir("activemq/CVE-2023-46604")
	h.compose.images[dir] = []string{"vulhub/activemq:5.17.3"}

	r := h.run("del", "-a", "-y", "1").requireCode(t, 0)
	r.requireOut(t, "vulhub/activemq:5.17.3")

	if len(h.compose.downCalls) != 1 || !h.compose.downCalls[0].RemoveVolumes {
		t.Fatalf("-a 应连带删除 volume：%+v", h.compose.downCalls)
	}
	if len(h.compose.removed) != 1 || len(h.compose.removed[0]) != 1 || h.compose.removed[0][0] != "vulhub/activemq:5.17.3" {
		t.Fatalf("-a 应删除该靶场引用的镜像：%v", h.compose.removed)
	}
}

func TestDelWithoutAllLeavesImagesAlone(t *testing.T) {
	h := newHarness(t)
	h.seed("activemq/CVE-2023-46604=Apache ActiveMQ RCE")
	dir := h.dir("activemq/CVE-2023-46604")
	h.compose.images[dir] = []string{"vulhub/activemq:5.17.3"}

	h.run("del", "-y", "1").requireCode(t, 0)
	if len(h.compose.removed) != 0 {
		t.Fatalf("未指定 -a 时不应删除镜像：%v", h.compose.removed)
	}
}

// 镜像被其他容器占用时 Docker 会拒绝删除，这是预期内的结果，不该让整个 del 失败。
func TestDelAllReportsImageRemovalFailureWithoutFailing(t *testing.T) {
	h := newHarness(t)
	h.seed("activemq/CVE-2023-46604=Apache ActiveMQ RCE")
	dir := h.dir("activemq/CVE-2023-46604")
	h.compose.images[dir] = []string{"vulhub/activemq:5.17.3"}
	h.compose.removeErr = errors.New("image is being used by running container")

	r := h.run("del", "-a", "-y", "1").requireCode(t, 0).requireOut(t, "已销毁")
	r.requireErr(t, "镜像未能删除")
}

// del 是唯一不可逆的操作，一次只能销毁一个靶场。多选必须当作用法错误挡在门外，
// 而不是静默地只销毁第一个。
func TestDelRejectsMultipleEnvironments(t *testing.T) {
	h := newHarness(t)
	h.seed(
		"activemq/CVE-2023-46604=Apache ActiveMQ RCE",
		"log4j/CVE-2021-44228=Log4j2 RCE",
	)

	h.run("del", "-y", "1,2").requireCode(t, 2)
	if len(h.compose.downCalls) != 0 {
		t.Fatalf("多选必须被拒绝：%v", h.compose.downCalls)
	}
}

func TestDelRejectsUnknownOption(t *testing.T) {
	h := newHarness(t)
	h.seed("activemq/CVE-2023-46604=Apache ActiveMQ RCE")
	h.run("del", "--force", "1").requireCode(t, 2)
}

func TestDelWithoutArgumentsIsUsageError(t *testing.T) {
	h := newHarness(t)
	h.seed("activemq/CVE-2023-46604=Apache ActiveMQ RCE")
	h.run("del").requireCode(t, 2)
}

// story 23 要求打印"容器和网络"。网络名由 compose 项目名决定，所以要把它说出来。
func TestDelNamesTheComposeProjectAndItsNetwork(t *testing.T) {
	h := newHarness(t)
	h.seed("activemq/CVE-2023-46604=Apache ActiveMQ RCE")
	h.compose.containers = []compose.Container{{
		Name:    "activemq-web-1",
		Project: "activemq-cve-2023-46604",
		WorkDir: h.dir("activemq/CVE-2023-46604"),
	}}

	r := h.run("del", "-y", "1").requireCode(t, 0)
	r.requireOut(t, "activemq-cve-2023-46604")
	r.requireOut(t, "网络")
}

// 无法确认在跑什么时不能继续：这是不可逆操作，
// 打印"没有运行中的容器"而实际有东西在跑，比直接失败危险得多。
func TestDelAbortsWhenItCannotDetermineWhatIsRunning(t *testing.T) {
	h := newHarness(t)
	h.seed("activemq/CVE-2023-46604=Apache ActiveMQ RCE")
	h.compose.containerErr = errors.New("Cannot connect to the Docker daemon")

	r := h.run("del", "-y", "1").requireCode(t, 1)
	r.requireErr(t, "无法确认")
	r.requireNoOut(t, "没有运行中的容器")
	if len(h.compose.downCalls) != 0 {
		t.Fatalf("无法确认运行状态时不应销毁：%v", h.compose.downCalls)
	}
}

func TestDelAnnouncesWhatWillBeRemoved(t *testing.T) {
	h := newHarness(t)
	h.seed("activemq/CVE-2023-46604=Apache ActiveMQ RCE")
	h.compose.containers = []compose.Container{{
		Name: "activemq-cve-2023-46604-web-1", WorkDir: h.dir("activemq/CVE-2023-46604"),
	}}

	h.run("del", "-y", "1").requireCode(t, 0).requireOut(t, "activemq-cve-2023-46604-web-1")
}

// writeIndex 直接落一份索引文件，用于构造"编号已分配但靶场已消失"这类状态。
func (h *harness) writeIndex(payload string) {
	h.t.Helper()
	if err := os.MkdirAll(filepath.Dir(h.index), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(h.index, []byte(payload), 0o644); err != nil {
		h.t.Fatal(err)
	}
}
