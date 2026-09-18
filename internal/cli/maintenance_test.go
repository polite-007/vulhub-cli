package cli_test

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/polite-007/vulhub-cli/internal/compose"
)

// --- init ---

func TestInitRefusesWhenTheCheckoutAlreadyExists(t *testing.T) {
	h := newHarness(t)
	h.seed("activemq/CVE-2023-46604=Apache ActiveMQ RCE")

	h.run("init").requireCode(t, 1).requireErr(t, "已存在")
	if len(h.git.cloneCalls) != 0 {
		t.Fatalf("检出已存在时不应克隆：%v", h.git.cloneCalls)
	}
}

func TestInitReportsMissingGit(t *testing.T) {
	h := newHarness(t)
	h.git.checkErr = errors.New("找不到 git 命令，请先安装 git")

	h.run("init").requireCode(t, 1).requireErr(t, "找不到 git 命令")
	if len(h.git.cloneCalls) != 0 {
		t.Fatalf("前置检查失败时不应克隆：%v", h.git.cloneCalls)
	}
}

func TestInitReportsMissingDocker(t *testing.T) {
	h := newHarness(t)
	h.compose.checkErr = errors.New("找不到 docker 命令，请先安装 Docker")

	h.run("init").requireCode(t, 1).requireErr(t, "找不到 docker 命令")
	if len(h.git.cloneCalls) != 0 {
		t.Fatalf("前置检查失败时不应克隆：%v", h.git.cloneCalls)
	}
}

func TestInitClonesShallowAndAssignsNumbers(t *testing.T) {
	h := newHarness(t)
	h.git.cloneFn = func(_, dest string) error {
		h.seed("activemq/CVE-2023-46604=Apache ActiveMQ RCE", "log4j/CVE-2021-44228=Log4j2 RCE")
		return nil
	}

	h.run("init").requireCode(t, 0).requireOut(t, "共 2 个靶场")

	if len(h.git.cloneCalls) != 1 {
		t.Fatalf("期望克隆 1 次，实际 %d 次", len(h.git.cloneCalls))
	}
	call := h.git.cloneCalls[0]
	if call.Dest != h.root {
		t.Fatalf("克隆目标 = %q, 期望 %q", call.Dest, h.root)
	}
	if call.Depth != 1 {
		t.Fatalf("应当是浅克隆（depth 1），实际 depth = %d", call.Depth)
	}

	// init 完成后编号就该可用，不必先跑一次 ls。
	index := h.indexFile()
	if !strings.Contains(index, "activemq/CVE-2023-46604") || !strings.Contains(index, "log4j/CVE-2021-44228") {
		t.Fatalf("init 后索引未包含全部靶场：\n%s", index)
	}
}

// --- update ---

func TestUpdateAppendsNumbersForNewEnvironments(t *testing.T) {
	h := newHarness(t)
	h.seed("activemq/CVE-2023-46604=Apache ActiveMQ RCE", "log4j/CVE-2021-44228=Log4j2 RCE")
	h.run("ls").requireCode(t, 0) // 分配 1、2

	h.seed(
		"activemq/CVE-2023-46604=Apache ActiveMQ RCE",
		"log4j/CVE-2021-44228=Log4j2 RCE",
		"zabbix/CVE-2022-23131=Zabbix RCE",
	)
	h.run("update").requireCode(t, 0)

	index := h.indexFile()
	if !strings.Contains(index, `"zabbix/CVE-2022-23131": 3`) {
		t.Fatalf("新靶场应被追加到编号末尾：\n%s", index)
	}
}

// 消失的靶场其编号作废且不复用：否则"68 号"在同事之间就没法口头传达了。
func TestUpdateDoesNotReuseTheNumberOfARemovedEnvironment(t *testing.T) {
	h := newHarness(t)
	h.seed(
		"a/one=One",
		"b/two=Two",
		"c/three=Three",
	)
	h.run("ls").requireCode(t, 0) // 分配 1、2、3

	if err := os.RemoveAll(h.dir("b/two")); err != nil {
		t.Fatal(err)
	}
	h.seed("a/one=One", "c/three=Three", "d/four=Four")
	h.run("update").requireCode(t, 0)

	index := h.indexFile()
	if !strings.Contains(index, `"d/four": 4`) {
		t.Fatalf("新靶场应拿到 4 号而不是复用被释放的 2 号：\n%s", index)
	}
	if strings.Contains(index, `"d/four": 2`) {
		t.Fatalf("2 号被复用了：\n%s", index)
	}
}

// 工具不替用户做合并决策：冲突时把 git 的原始输出原样带出来。
func TestUpdateSurfacesGitOutputOnConflict(t *testing.T) {
	h := newHarness(t)
	h.seed("a/one=One")
	h.run("ls").requireCode(t, 0)
	h.git.pullErr = errors.New("git pull: exit status 1\nCONFLICT (content): Merge conflict in a/one/docker-compose.yml")

	r := h.run("update").requireCode(t, 1)
	r.requireErr(t, "CONFLICT")
}

func TestUpdateReportsRunningEnvironmentsThatChanged(t *testing.T) {
	h := newHarness(t)
	h.seed("a/one=One", "b/two=Two", "c/three=Three")
	h.run("ls").requireCode(t, 0)

	h.git.changed = []string{"a/one/docker-compose.yml"}
	h.compose.containers = []compose.Container{
		{Name: "a-one-1", WorkDir: h.dir("a/one")},
		{Name: "b-two-1", WorkDir: h.dir("b/two")},
	}

	r := h.run("update").requireCode(t, 0)
	r.requireOut(t, "受本次更新影响")
	r.requireOut(t, "\n  a/one\n")
	if strings.Contains(r.out, "\n  b/two\n") {
		t.Fatalf("未改动的运行中靶场不该出现在受影响列表里：\n%s", r.out)
	}
}

func TestUpdateWithoutCheckoutTellsUserToInit(t *testing.T) {
	h := newHarness(t)
	h.run("update").requireCode(t, 1).requireErr(t, "vulhub init")
}

// --- pull ---

func TestPullAcceptsCommaSeparatedEnvironments(t *testing.T) {
	h := newHarness(t)
	h.seed("a/one=One", "b/two=Two", "c/three=Three")

	h.run("pull", "1,3").requireCode(t, 0)

	if len(h.compose.pullCalls) != 2 {
		t.Fatalf("期望 Pull 被调用 2 次，实际 %d 次：%v", len(h.compose.pullCalls), h.compose.pullCalls)
	}
}

func TestPullAllPullsEveryEnvironment(t *testing.T) {
	h := newHarness(t)
	h.seed("a/one=One", "b/two=Two", "c/three=Three")

	h.run("pull", "--all").requireCode(t, 0)
	if len(h.compose.pullCalls) != 3 {
		t.Fatalf("--all 应拉取全部 3 个靶场，实际 %d 次", len(h.compose.pullCalls))
	}
}

func TestPullWithoutArgumentsIsUsageError(t *testing.T) {
	h := newHarness(t)
	h.seed("a/one=One")
	h.run("pull").requireCode(t, 2)
}

func TestPullRejectsAllCombinedWithTargets(t *testing.T) {
	h := newHarness(t)
	h.seed("a/one=One")
	h.run("pull", "--all", "1").requireCode(t, 1)
	if len(h.compose.pullCalls) != 0 {
		t.Fatalf("冲突的参数组合不应触发任何拉取：%v", h.compose.pullCalls)
	}
}

// --- status ---

func TestStatusGroupsContainersByEnvironment(t *testing.T) {
	h := newHarness(t)
	h.seed("a/one=One", "b/two=Two")
	h.run("ls").requireCode(t, 0)

	h.compose.containers = []compose.Container{
		{
			Name: "a-one-web-1", Service: "web", State: "running", WorkDir: h.dir("a/one"),
			Ports: []compose.PortMapping{{HostPort: 8080, ContainerPort: 80, Protocol: "tcp"}},
		},
		{Name: "a-one-db-1", Service: "db", State: "running", WorkDir: h.dir("a/one")},
	}

	r := h.run("status").requireCode(t, 0)
	r.requireOut(t, "a/one")
	r.requireOut(t, "a-one-web-1")
	r.requireOut(t, "a-one-db-1")
	r.requireOut(t, "8080→80/tcp")
	r.requireNoOut(t, "b/two")
}

func TestStatusWithNoRunningEnvironmentsSaysSo(t *testing.T) {
	h := newHarness(t)
	h.seed("a/one=One")
	h.run("status").requireCode(t, 0).requireOut(t, "当前没有运行中的靶场")
}

func TestStatusJSONOutputsStructuredData(t *testing.T) {
	h := newHarness(t)
	h.seed("a/one=One")
	h.run("ls").requireCode(t, 0)

	h.compose.containers = []compose.Container{{
		Name: "a-one-web-1", Service: "web", State: "running", WorkDir: h.dir("a/one"),
		Ports: []compose.PortMapping{{HostPort: 8080, ContainerPort: 80, Protocol: "tcp"}},
	}}

	r := h.run("status", "--json").requireCode(t, 0)
	var payload []struct {
		Number     int    `json:"number"`
		Path       string `json:"path"`
		Name       string `json:"name"`
		Containers []struct {
			Name    string `json:"name"`
			Service string `json:"service"`
			State   string `json:"state"`
			Ports   []struct {
				HostPort      int    `json:"host_port"`
				ContainerPort int    `json:"container_port"`
				Protocol      string `json:"protocol"`
			} `json:"ports"`
		} `json:"containers"`
	}
	if err := json.Unmarshal([]byte(r.out), &payload); err != nil {
		t.Fatalf("--json 输出不是合法 JSON：%v\n%s", err, r.out)
	}
	if len(payload) != 1 || payload[0].Path != "a/one" || payload[0].Number != 1 {
		t.Fatalf("--json 内容不符：%+v", payload)
	}
	if len(payload[0].Containers) != 1 || payload[0].Containers[0].Ports[0].HostPort != 8080 {
		t.Fatalf("--json 的容器内容不符：%+v", payload[0].Containers)
	}
}

// vulhub 尚未初始化时 status 仍应可用：它看的是全机容器，不依赖本地检出。
func TestStatusWorksBeforeInit(t *testing.T) {
	h := newHarness(t)
	h.compose.containers = []compose.Container{{
		Name: "a-one-web-1", Service: "web", State: "running", WorkDir: h.dir("a/one"),
	}}

	r := h.run("status").requireCode(t, 0)
	r.requireOut(t, "a/one")
	r.requireOut(t, "a-one-web-1")
}

func TestStatusRejectsUnknownOption(t *testing.T) {
	h := newHarness(t)
	h.run("status", "--wat").requireCode(t, 2)
}
