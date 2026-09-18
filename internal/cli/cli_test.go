package cli_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRunWithoutArgumentsShowsUsage(t *testing.T) {
	h := newHarness(t)
	r := h.run().requireCode(t, 2)
	if !strings.Contains(r.err, "用法") {
		t.Fatalf("stderr 未包含用法说明：%s", r.err)
	}
}

func TestRunWithUnknownCommandShowsUsage(t *testing.T) {
	h := newHarness(t)
	h.run("frobnicate").requireCode(t, 2).requireErr(t, "未知命令")
}

func TestLsListsEnvironmentsWithNumbersAndTitles(t *testing.T) {
	h := newHarness(t)
	h.seed(
		"activemq/CVE-2023-46604=Apache ActiveMQ RCE",
		"log4j/CVE-2021-44228=Log4j2 RCE",
	)

	r := h.run("ls").requireCode(t, 0)
	r.requireOut(t, "activemq/CVE-2023-46604").requireOut(t, "Apache ActiveMQ RCE")
	r.requireOut(t, "log4j/CVE-2021-44228").requireOut(t, "Log4j2 RCE")
}

func TestLsOrdersEnvironmentsLexicographically(t *testing.T) {
	h := newHarness(t)
	h.seed(
		"zabbix/CVE-2022-23131=Zabbix",
		"activemq/CVE-2023-46604=ActiveMQ",
		"nginx/CVE-2017-7529=Nginx",
	)

	r := h.run("ls").requireCode(t, 0)
	activemq := strings.Index(r.out, "activemq/CVE-2023-46604")
	nginx := strings.Index(r.out, "nginx/CVE-2017-7529")
	zabbix := strings.Index(r.out, "zabbix/CVE-2022-23131")
	if !(activemq < nginx && nginx < zabbix) {
		t.Fatalf("靶场未按路径字典序排列：\n%s", r.out)
	}
	if !(activemq >= 0) {
		t.Fatalf("缺少 activemq：\n%s", r.out)
	}
}

// 降级路径：environments.toml 缺失时扫描目录树，此时没有标题可显示。
func TestLsFallsBackToDirectoryScanWithoutRegistry(t *testing.T) {
	h := newHarness(t)
	h.seedWithoutRegistry("activemq/CVE-2023-46604", "base/skipme", "tests/alsoskip")

	r := h.run("ls").requireCode(t, 0)
	r.requireOut(t, "activemq/CVE-2023-46604")
	r.requireNoOut(t, "base/")
	r.requireNoOut(t, "tests/")
}

func TestLsJSONOutputsStructuredData(t *testing.T) {
	h := newHarness(t)
	h.seed("activemq/CVE-2023-46604=Apache ActiveMQ RCE")

	r := h.run("ls", "--json").requireCode(t, 0)
	var payload []struct {
		Number int    `json:"number"`
		Path   string `json:"path"`
		Name   string `json:"name"`
	}
	if err := json.Unmarshal([]byte(r.out), &payload); err != nil {
		t.Fatalf("--json 输出不是合法 JSON：%v\n%s", err, r.out)
	}
	if len(payload) != 1 || payload[0].Path != "activemq/CVE-2023-46604" || payload[0].Number != 1 {
		t.Fatalf("--json 内容不符：%+v", payload)
	}
}

func TestSearchMatchesByPathSoCVENumberIsFindable(t *testing.T) {
	h := newHarness(t)
	h.seed(
		"activemq/CVE-2023-46604=Apache ActiveMQ RCE",
		"log4j/CVE-2021-44228=Log4j2 RCE",
	)

	r := h.run("search", "CVE-2021-44228").requireCode(t, 0)
	r.requireOut(t, "log4j/CVE-2021-44228").requireNoOut(t, "activemq/CVE-2023-46604")
}

func TestSearchMatchesByTitle(t *testing.T) {
	h := newHarness(t)
	h.seed(
		"activemq/CVE-2023-46604=Apache ActiveMQ RCE",
		"log4j/CVE-2021-44228=Log4j2 RCE",
	)

	r := h.run("search", "ActiveMQ").requireCode(t, 0)
	r.requireOut(t, "activemq/CVE-2023-46604").requireNoOut(t, "log4j/CVE-2021-44228")
}

func TestSearchIsCaseInsensitive(t *testing.T) {
	h := newHarness(t)
	h.seed("log4j/CVE-2021-44228=Log4j2 Remote Code Execution")
	h.run("search", "log4j2").requireCode(t, 0).requireOut(t, "log4j/CVE-2021-44228")
	h.run("search", "LOG4J2").requireCode(t, 0).requireOut(t, "log4j/CVE-2021-44228")
}

func TestSearchRequiresAllKeywordsToMatch(t *testing.T) {
	h := newHarness(t)
	h.seed(
		"activemq/CVE-2023-46604=Apache ActiveMQ RCE",
		"log4j/CVE-2021-44228=Log4j2 RCE",
	)

	// 两个词都命中 activemq，log4j 缺 apache。
	r := h.run("search", "apache", "rce").requireCode(t, 0)
	r.requireOut(t, "activemq/CVE-2023-46604").requireNoOut(t, "log4j/CVE-2021-44228")
}

func TestSearchWithoutMatchesReturnsNonZeroExitCode(t *testing.T) {
	h := newHarness(t)
	h.seed("activemq/CVE-2023-46604=Apache ActiveMQ RCE")
	h.run("search", "nonexistent-keyword").requireCode(t, 1)
}

func TestSearchWithoutKeywordsIsUsageError(t *testing.T) {
	h := newHarness(t)
	h.run("search").requireCode(t, 2)
}

// --- 分页 ---
//
// 分页有两面：不是终端时必须完全不读输入（否则 `vulhub ls | grep xxx` 会挂住），
// 是终端时首屏放行 20 行、此后每行读一次输入。

// countingReader 每次 Read 返回一个换行，并记下被读了几次。
type countingReader struct{ reads int }

func (r *countingReader) Read(p []byte) (int, error) {
	r.reads++
	if len(p) == 0 {
		return 0, nil
	}
	p[0] = '\n'
	return 1, nil
}

func TestPagerIsDisabledWhenOutputIsNotATerminal(t *testing.T) {
	h := newHarness(t)
	h.seed(seedMany(25)...)

	in := &countingReader{}
	h.runInput(in, false, "ls").requireCode(t, 0)

	if in.reads != 0 {
		t.Fatalf("非终端时不应读取输入，实际读取 %d 次", in.reads)
	}
}

func TestPagerReadsOneLinePerRowAfterTheFirstScreen(t *testing.T) {
	h := newHarness(t)
	h.seed(seedMany(25)...)

	in := &countingReader{}
	r := h.runInput(in, true, "ls").requireCode(t, 0)

	// 首屏 20 行放行，剩下 5 行各要一次输入。
	if in.reads != 5 {
		t.Fatalf("期望读输入 5 次（25 行 - 首屏 20 行），实际 %d 次\n%s", in.reads, r.out)
	}
	if lines := strings.Count(strings.TrimSpace(r.out), "\n") + 1; lines != 25 {
		t.Fatalf("期望输出 25 行，实际 %d 行\n%s", lines, r.out)
	}
}

func TestPagerKeepsGoingWhenInputIsExhausted(t *testing.T) {
	h := newHarness(t)
	h.seed(seedMany(25)...)

	// 输入立刻 EOF：分页必须放弃，把剩余内容全部放行，
	// 不能让用户被困在半截输出里。
	r := h.runInput(strings.NewReader(""), true, "ls").requireCode(t, 0)
	if lines := strings.Count(strings.TrimSpace(r.out), "\n") + 1; lines != 25 {
		t.Fatalf("输入耗尽时仍应输出全部 25 行，实际 %d 行\n%s", lines, r.out)
	}
}

// 回归测试：分页时按下 Ctrl+C 必须能退出。
//
// 缺陷的由来：main 用 signal.NotifyContext 拦截了 SIGINT，进程不再因 Ctrl+C
// 而死，改为取消一个 context；而分页阻塞在 Read 上，没有任何人监听那个
// context，于是进程永远卡住，Ctrl+C 也救不回来。
func TestLsExitsOnInterruptWhilePaging(t *testing.T) {
	h := newHarness(t)
	h.seed(seedMany(25)...)

	in := newBlockingReader()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan result, 1)
	go func() { done <- h.runInputCtx(ctx, in, true, "ls") }()

	select {
	case <-in.started:
	case <-time.After(5 * time.Second):
		t.Fatal("分页没有开始等待输入")
	}

	cancel()

	select {
	case r := <-done:
		// 130 = 128 + SIGINT，shell 惯例。
		r.requireCode(t, 130)
	case <-time.After(5 * time.Second):
		t.Fatal("取消之后 ls 仍未退出——这正是 Ctrl+C 无效的那个缺陷")
	}
}

// seedMany 生成 n 个靶场，路径按字典序可预期。
func seedMany(n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, pathN(i)+"=Target "+pathN(i))
	}
	return out
}

func pathN(i int) string {
	return "app" + string(rune('a'+i/10)) + string(rune('a'+i%10)) + "/CVE-2020-" + string(rune('a'+i%10))
}
