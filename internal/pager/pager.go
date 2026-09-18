// Package pager 提供 ls 与 search 的分页输出。
package pager

import (
	"fmt"
	"io"
)

// defaultPageSize 是首屏显示的行数。
const defaultPageSize = 20

// Pager 先放行首屏，此后每读到一个换行才放行下一行。
//
// 输出不是终端时（被管道接走、重定向到文件）分页自动关闭，
// 否则 `vulhub ls | grep xxx` 会挂住。
type Pager struct {
	out       io.Writer
	in        io.Reader
	enabled   bool
	pageSize  int
	written   int
	exhausted bool
}

// New 构造一个 Pager。enabled 为 false 时所有分页行为被跳过。
func New(out io.Writer, in io.Reader, enabled bool) *Pager {
	return &Pager{
		out:      out,
		in:       in,
		enabled:  enabled,
		pageSize: defaultPageSize,
	}
}

// Println 输出一行，必要时先等待用户按回车。
func (p *Pager) Println(line string) error {
	if p.enabled && !p.exhausted && p.written >= p.pageSize {
		if !waitForEnter(p.in) {
			// 输入已耗尽（EOF / 管道关闭）：停止分页，把剩余内容直接放行，
			// 不要让用户被困在半截输出里。
			p.exhausted = true
		}
	}
	if _, err := fmt.Fprintln(p.out, line); err != nil {
		return err
	}
	p.written++
	return nil
}

// waitForEnter 逐字节读到换行。逐字节是为了不在调用方的输入流上再套一层缓冲，
// 那会吞掉后续读取需要的字节。返回 false 表示输入已耗尽。
func waitForEnter(in io.Reader) bool {
	var buf [1]byte
	for {
		n, err := in.Read(buf[:])
		if n > 0 && buf[0] == '\n' {
			return true
		}
		if err != nil {
			return n > 0
		}
	}
}
