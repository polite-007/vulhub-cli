package cli_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/polite-007/vulhub-cli/internal/compose"
)

func TestLsListsEnvironmentsFromBothSources(t *testing.T) {
	h := newHarness(t)
	h.seed("activemq/CVE-2023-46604=Apache ActiveMQ RCE")
	h.seedVulfocus("drupal-cve_2018_7600=80,443")

	r := h.run("ls").requireCode(t, 0)
	r.requireOut(t, "activemq/CVE-2023-46604")
	r.requireOut(t, "vulfocus/drupal-cve_2018_7600")
	// 两个来源共用一套编号：谁先排到前面由路径字典序决定，不依次序断言。
	r.requireOut(t, "1  ")
	r.requireOut(t, "2  ")
}

// 标题必须从镜像名推导出来，否则按 CVE 号检索会零命中——
// 镜像名里是 cve_2018_7600 这种下划线形式。
func TestVulfocusTitlesAreDerivedFromImageNames(t *testing.T) {
	h := newHarness(t)
	h.seedVulfocus(
		"drupal-cve_2018_7600=80",
		"fastjson-cnvd_2017_02833=8090",
		"apache-druid-cve_2021_25646=8888",
		"redis-cve_2022_0543=6379",
		"some-image-without-an-id=8080",
	)

	r := h.run("ls", "--json").requireCode(t, 0)
	var payload []struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(r.out), &payload); err != nil {
		t.Fatalf("--json 输出不是合法 JSON：%v\n%s", err, r.out)
	}

	titles := map[string]string{}
	for _, item := range payload {
		titles[item.Path] = item.Name
	}

	want := map[string]string{
		"vulfocus/drupal-cve_2018_7600":        "Drupal CVE-2018-7600",
		"vulfocus/fastjson-cnvd_2017_02833":    "Fastjson CNVD-2017-02833",
		"vulfocus/apache-druid-cve_2021_25646": "Apache Druid CVE-2021-25646",
		"vulfocus/redis-cve_2022_0543":         "Redis CVE-2022-0543",
		// 名字里没有可识别的编号时原样返回，不做无根据的加工。
		"vulfocus/some-image-without-an-id": "some-image-without-an-id",
	}
	for path, expected := range want {
		if titles[path] != expected {
			t.Errorf("%s 的标题 = %q, 期望 %q", path, titles[path], expected)
		}
	}
}

// 编号有两种写法：下划线（drupal-cve_2018_7600）与连字符（log4j2-cve-2021-4104）。
// 后者曾经推导不出来，标题退化成原样的镜像名，于是按 CVE 号搜不到它——
// 而那正好是标题推导存在的理由。
func TestVulfocusTitlesHandleHyphenSeparatedIDs(t *testing.T) {
	h := newHarness(t)
	h.seedVulfocus(
		"log4j2-cve-2021-4104=8080",
		"confluence_cve-2022-26134=8090",
		"dedecms-cve_2015-4553=80",
		"log4j2-rce-2021-12-09=8080",
	)

	titles := map[string]string{}
	for path, name := range h.titlesByPath(h.run("ls", "--json").requireCode(t, 0)) {
		titles[path] = name
	}

	want := map[string]string{
		"vulfocus/log4j2-cve-2021-4104":      "Log4j2 CVE-2021-4104",
		"vulfocus/confluence_cve-2022-26134": "Confluence CVE-2022-26134",
		"vulfocus/dedecms-cve_2015-4553":     "Dedecms CVE-2015-4553",
	}
	for path, expected := range want {
		if titles[path] != expected {
			t.Errorf("%s 的标题 = %q, 期望 %q", path, titles[path], expected)
		}
	}

	// 而这些标题必须真的能被按 CVE 号搜到。
	r := h.run("search", "CVE-2021-4104").requireCode(t, 0)
	r.requireOut(t, "vulfocus/log4j2-cve-2021-4104")
}

// "-fix" 后缀表示已修复版本，不是可打的靶场。不标注的话它和漏洞版标题一样，
// 列表里根本分不出来。
func TestVulfocusTitlesMarkFixedVariants(t *testing.T) {
	h := newHarness(t)
	h.seedVulfocus(
		"vtiger-cve_2020_19363=80",
		"vtiger-cve_2020_19363-fix=80",
	)

	titles := h.titlesByPath(h.run("ls", "--json").requireCode(t, 0))

	vulnerable := titles["vulfocus/vtiger-cve_2020_19363"]
	fixed := titles["vulfocus/vtiger-cve_2020_19363-fix"]

	if vulnerable != "Vtiger CVE-2020-19363" {
		t.Errorf("漏洞版标题 = %q", vulnerable)
	}
	if fixed != "Vtiger CVE-2020-19363 (fixed)" {
		t.Errorf("已修复版标题 = %q", fixed)
	}
	if vulnerable == fixed {
		t.Fatal("漏洞版与已修复版的标题不能相同")
	}
}

func TestSearchFindsVulfocusEnvironmentByCVENumber(t *testing.T) {
	h := newHarness(t)
	h.seedVulfocus("drupal-cve_2018_7600=80", "redis-cve_2022_0543=6379")

	r := h.run("search", "CVE-2018-7600").requireCode(t, 0)
	r.requireOut(t, "vulfocus/drupal-cve_2018_7600")
	r.requireNoOut(t, "vulfocus/redis-cve_2022_0543")
}

// vulfocus 上游只有镜像、没有 compose 文件，环境定义由我们合成。
func TestUpSynthesizesComposeFileForVulfocusEnvironment(t *testing.T) {
	h := newHarness(t)
	h.seedVulfocus("drupal-cve_2018_7600=80,443")

	h.run("up", "vulfocus/drupal-cve_2018_7600").requireCode(t, 0)

	got := h.composeFileOf("drupal-cve_2018_7600")
	for _, want := range []string{
		"image: vulfocus/drupal-cve_2018_7600",
		`"80:80"`,
		`"443:443"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("合成的 compose 文件缺少 %q：\n%s", want, got)
		}
	}

	// 镜像自带 Entrypoint/Cmd，会自己把服务起起来，所以不该替它指定 command。
	if strings.Contains(got, "command:") {
		t.Errorf("不该为 vulfocus 镜像指定 command：\n%s", got)
	}
}

func TestUpOnVulfocusPassesTheSynthesizedDirectory(t *testing.T) {
	h := newHarness(t)
	h.seedVulfocus("redis-cve_2022_0543=6379")

	h.run("up", "vulfocus/redis-cve_2022_0543").requireCode(t, 0)

	if len(h.compose.upCalls) != 1 {
		t.Fatalf("期望 Up 被调用 1 次，实际 %d 次", len(h.compose.upCalls))
	}
	if want := h.vulfocusDir("redis-cve_2022_0543"); h.compose.upCalls[0] != want {
		t.Fatalf("Up 收到的目录 = %q, 期望 %q", h.compose.upCalls[0], want)
	}
}

func TestUpOnVulfocusResolvesByNumber(t *testing.T) {
	h := newHarness(t)
	h.seedVulfocus("redis-cve_2022_0543=6379")

	// 编号在清单里只有一个，必然是 1。
	h.run("up", "1").requireCode(t, 0)
	if len(h.compose.upCalls) != 1 || h.compose.upCalls[0] != h.vulfocusDir("redis-cve_2022_0543") {
		t.Fatalf("编号 1 未解析到 vulfocus 靶场：%v", h.compose.upCalls)
	}
}

func TestStopOnVulfocusEnvironment(t *testing.T) {
	h := newHarness(t)
	h.seedVulfocus("redis-cve_2022_0543=6379")

	h.run("stop", "vulfocus/redis-cve_2022_0543").requireCode(t, 0)
	if len(h.compose.stopCalls) != 1 || h.compose.stopCalls[0] != h.vulfocusDir("redis-cve_2022_0543") {
		t.Fatalf("Stop 未作用在合成目录上：%v", h.compose.stopCalls)
	}
}

func TestDelOnVulfocusEnvironment(t *testing.T) {
	h := newHarness(t)
	h.seedVulfocus("redis-cve_2022_0543=6379")
	dir := h.vulfocusDir("redis-cve_2022_0543")
	h.compose.images[dir] = []string{"vulfocus/redis-cve_2022_0543"}

	h.run("del", "-a", "-y", "vulfocus/redis-cve_2022_0543").requireCode(t, 0)

	if len(h.compose.downCalls) != 1 || h.compose.downCalls[0].Dir != dir {
		t.Fatalf("Down 未作用在合成目录上：%+v", h.compose.downCalls)
	}
	if len(h.compose.removed) != 1 || h.compose.removed[0][0] != "vulfocus/redis-cve_2022_0543" {
		t.Fatalf("-a 未删除镜像：%v", h.compose.removed)
	}
}

func TestPullOnVulfocusEnvironment(t *testing.T) {
	h := newHarness(t)
	h.seedVulfocus("redis-cve_2022_0543=6379")

	h.run("pull", "vulfocus/redis-cve_2022_0543").requireCode(t, 0)
	if len(h.compose.pullCalls) != 1 || h.compose.pullCalls[0] != h.vulfocusDir("redis-cve_2022_0543") {
		t.Fatalf("Pull 未作用在合成目录上：%v", h.compose.pullCalls)
	}
}

func TestUnknownVulfocusImageIsRejected(t *testing.T) {
	h := newHarness(t)
	h.seedVulfocus("drupal-cve_2018_7600=80")

	h.run("up", "vulfocus/never-heard-of-it").requireCode(t, 1).requireErr(t, "找不到靶场")
}

// vulfocus 清单内嵌在二进制里，所以没有 vulhub 检出时它照样可用。
func TestVulfocusEnvironmentsAreAvailableWithoutVulhubCheckout(t *testing.T) {
	h := newHarness(t)
	h.seedVulfocus("redis-cve_2022_0543=6379")

	r := h.run("ls").requireCode(t, 0)
	r.requireOut(t, "vulfocus/redis-cve_2022_0543")
}

// -time 按创建时间倒序显示，新的在前。
func TestLsTimeShowsNewestFirst(t *testing.T) {
	h := newHarness(t)
	h.seedVulfocus(
		"old-cve_2015_0001=80@2015-01-01",
		"newest-cve_2024_0002=80@2024-01-01",
		"middle-cve-2020-0003=80@2020-06-01",
	)
	h.seed("activemq/CVE-2023-46604=Apache ActiveMQ RCE")

	r := h.run("ls", "-time").requireCode(t, 0)

	newest := strings.Index(r.out, "vulfocus/newest-cve_2024_0002")
	middle := strings.Index(r.out, "vulfocus/middle-cve-2020-0003")
	old := strings.Index(r.out, "vulfocus/old-cve_2015_0001")
	// vulhub 没有创建时间，排在最后。
	vulhub := strings.Index(r.out, "activemq/CVE-2023-46604")

	if !(newest >= 0 && newest < middle && middle < old && old < vulhub) {
		t.Fatalf("-time 未按创建时间倒序排列（vulhub 应排最后）：\n%s", r.out)
	}
}

// -time 只影响显示，**绝不能动编号**。
//
// 编号按路径字典序一次性分配、此后不变，这是"记住 68 号、写进脚本、
// 口头传达给同事"能成立的前提。一旦显示顺序能改动编号，那个前提就没了。
func TestLsTimeDoesNotChangeNumbers(t *testing.T) {
	h := newHarness(t)
	h.seedVulfocus(
		"old-cve_2015_0001=80@2015-01-01",
		"newest-cve_2024_0002=80@2024-01-01",
		"middle-cve-2020-0003=80@2020-06-01",
	)
	h.seed("activemq/CVE-2023-46604=Apache ActiveMQ RCE")

	byDefault := h.numbersByPath(h.run("ls", "--json").requireCode(t, 0))
	byTime := h.numbersByPath(h.run("ls", "-time", "--json").requireCode(t, 0))

	if len(byDefault) != len(byTime) {
		t.Fatalf("两种排序下的靶场数量不同：%d vs %d", len(byDefault), len(byTime))
	}
	for path, number := range byDefault {
		if byTime[path] != number {
			t.Errorf("%s 的编号在 -time 下变了：%d → %d", path, number, byTime[path])
		}
	}
}

// 没有创建时间的靶场不能因为排序而消失或错位。
func TestLsTimeKeepsTimelessEnvironmentsAtTheEnd(t *testing.T) {
	h := newHarness(t)
	h.seedVulfocus("dated-cve_2024_0001=80@2024-01-01")
	h.seed("activemq/CVE-2023-46604=Apache ActiveMQ RCE", "log4j/CVE-2021-44228=Log4j2 RCE")

	r := h.run("ls", "-time").requireCode(t, 0)
	dated := strings.Index(r.out, "vulfocus/dated-cve_2024_0001")
	firstVulhub := strings.Index(r.out, "activemq/CVE-2023-46604")
	secondVulhub := strings.Index(r.out, "log4j/CVE-2021-44228")

	if !(dated < firstVulhub && firstVulhub < secondVulhub) {
		t.Fatalf("无时间的靶场应排在最后且保持原有相对顺序：\n%s", r.out)
	}
}

// -time 也适用于 search。
func TestSearchTimeSortsNewestFirst(t *testing.T) {
	h := newHarness(t)
	h.seedVulfocus(
		"aaa-cve_2015_0001=80@2015-01-01",
		"bbb-cve_2024_0002=80@2024-01-01",
	)

	r := h.run("search", "cve", "-time").requireCode(t, 0)
	b := strings.Index(r.out, "vulfocus/bbb-cve_2024_0002")
	a := strings.Index(r.out, "vulfocus/aaa-cve_2015_0001")
	if !(b >= 0 && a >= 0 && b < a) {
		t.Fatalf("search -time 未按创建时间倒序：\n%s", r.out)
	}
}

// gen-vulfocus 要联网，因此它的主路径不进主测试套件——它属于"发布前手工跑的
// 冒烟测试"那一类。但参数解析不碰网络，可以在这里覆盖。
func TestGenVulfocusRejectsUnknownOption(t *testing.T) {
	h := newHarness(t)
	h.run("gen-vulfocus", "--bogus").requireCode(t, 2).requireErr(t, "未知选项")
}

func TestGenVulfocusRequiresAValueForEachOption(t *testing.T) {
	h := newHarness(t)
	for _, arg := range []string{"-o", "--output", "--username", "--token"} {
		h.run("gen-vulfocus", arg).requireCode(t, 2).requireErr(t, "需要一个参数")
	}
}

func TestStatusGroupsVulfocusEnvironmentLikeAnyOther(t *testing.T) {
	h := newHarness(t)
	h.seedVulfocus("redis-cve_2022_0543=6379")
	h.compose.containers = []compose.Container{{
		Name:    "redis-cve_2022_0543-target-1",
		Service: "target",
		State:   "running",
		WorkDir: h.vulfocusDir("redis-cve_2022_0543"),
		Ports:   []compose.PortMapping{{HostPort: 6379, ContainerPort: 6379, Protocol: "tcp"}},
	}}

	r := h.run("status").requireCode(t, 0)
	r.requireOut(t, "vulfocus/redis-cve_2022_0543")
	r.requireOut(t, "6379→6379/tcp")
}
