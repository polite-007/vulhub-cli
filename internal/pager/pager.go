// Package pager 提供 ls 与 search 的分页输出。
package pager

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
)

// defaultPageSize 是首屏显示的行数。
const defaultPageSize = 20

// ErrInterrupted 表示等待用户输入期间被取消，通常是 Ctrl+C。
var ErrInterrupted = errors.New("分页被中断")

// Pager 先放行首屏，此后每读到一个换行才放行下一行。
//
// 输出不是终端时（被管道接走、重定向到文件）分页自动关闭，
// 否则 `vulhub ls | grep xxx` 会挂住。
type Pager struct {
	ctx      context.Context
	out      io.Writer
	in       io.Reader
	enabled  bool
	pageSize int

	written     int
	exhausted   bool
	interrupted bool

	requests chan struct{}
	keys     chan byte
	readOnce sync.Once
}

// New 构造一个 Pager。enabled 为 false 时所有分页行为被跳过。
//
// ctx 必须传入进程的信号 context：等待用户按键是一个阻塞操作，
// 若不能被打断，Ctrl+C 就退不出去了。
func New(ctx context.Context, out io.Writer, in io.Reader, enabled bool) *Pager {
	return &Pager{
		ctx:      ctx,
		out:      out,
		in:       in,
		enabled:  enabled,
		pageSize: defaultPageSize,
	}
}

// Println 输出一行，必要时先等待用户按回车。
func (p *Pager) Println(line string) error {
	if p.interrupted {
		return ErrInterrupted
	}

	if p.enabled && !p.exhausted && p.written >= p.pageSize {
		if !p.waitForEnter() {
			// 输入耗尽时停止分页，把剩余内容直接放行，
			// 不要让用户被困在半截输出里。
			p.exhausted = true
		}
		if p.ctx.Err() != nil {
			p.interrupted = true
			return ErrInterrupted
		}
	}

	if _, err := fmt.Fprintln(p.out, line); err != nil {
		return err
	}
	p.written++
	return nil
}

// waitForEnter 等到用户按一次回车。返回 false 表示输入已耗尽或被取消，
// 调用方需自行区分这两种情况——用 ctx.Err()。
func (p *Pager) waitForEnter() bool {
	p.startReading()
	for {
		// 按需索取：先请求一个字节，再等它回来。这样读取不会跑在等待之前，
		// 否则会预读并吞掉一个本不属于本次等待的按键。
		select {
		case p.requests <- struct{}{}:
		case <-p.ctx.Done():
			return false
		}

		select {
		case b, ok := <-p.keys:
			if !ok {
				return false
			}
			// 只认回车：用户敲了别的键就忽略，与"按 enter 出现下一行"一致。
			if b == '\n' || b == '\r' {
				return true
			}
		case <-p.ctx.Done():
			return false
		}
	}
}

// startReading 起一个 goroutine，按请求读取输入并送到 keys 上。
//
// 底层的 Read 无法中断，所以必须把它放到 goroutine 里，由 waitForEnter 在
// ctx 取消时放行——否则 Ctrl+C 会卡在 Read 上出不来。
func (p *Pager) startReading() {
	p.readOnce.Do(func() {
		p.requests = make(chan struct{})
		p.keys = make(chan byte)
		go func() {
			defer close(p.keys)
			const maxEmptyReads = 1000 // Read 反复返回 (0, nil) 时不要空转
			var buf [1]byte
			emptyReads := 0
			for {
				select {
				case _, ok := <-p.requests:
					if !ok {
						return
					}
				case <-p.ctx.Done():
					return
				}

				n, err := p.in.Read(buf[:])
				if n > 0 {
					emptyReads = 0
					select {
					case p.keys <- buf[0]:
					case <-p.ctx.Done():
						return
					}
				} else if err == nil {
					if emptyReads++; emptyReads >= maxEmptyReads {
						return
					}
				}
				if err != nil {
					return
				}
			}
		}()
	})
}
