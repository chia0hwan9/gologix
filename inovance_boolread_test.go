package gologix

import (
	"context"
	"strings"
	"testing"
	"time"
)

// 汇川 default 对齐下，BOOL 结构体成员在线路上是 2 字节（文档 §4.4.2(1)，LSB 有效），
// 顶层 BOOL[n] 是每元素 1 字节（文档 §4.2）。逐元素读时前者每元素要吃掉 1 个 padding 字节。

func TestInvBoolElementSize(t *testing.T) {
	def := newInovanceClient(AlignDefault)
	ino := newInovanceClient(AlignInoProShop)

	tests := []struct {
		name string
		c    *Client
		tag  string
		want int
	}{
		{name: "default 成员标量", c: def, tag: "Stru.flag", want: 2},
		{name: "default 成员数组元素", c: def, tag: "application__gvl__Stru.flags[3]", want: 2},
		{name: "default 顶层标量", c: def, tag: "MyBool", want: 1},
		{name: "default 顶层数组", c: def, tag: "application__gvl__MyBoolArray", want: 1},
		{name: "inoproshop 成员", c: ino, tag: "Stru.flag", want: 1},
		{name: "inoproshop 顶层", c: ino, tag: "MyBool", want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.c.invBoolElementSize(tt.tag); got != tt.want {
				t.Errorf("invBoolElementSize(%q) = %d, want %d", tt.tag, got, tt.want)
			}
		})
	}
}

type readListResult struct {
	vals []any
	err  error
}

// 成员 BOOL 数组逐元素读：每个元素 2 字节（值 + padding），必须跳过 padding，
// 否则第二个元素起全部错位。
func TestReadListInovanceBoolMemberSkipsPadding(t *testing.T) {
	client, fs := newFakeCIPClient(t)
	client.Dialect = DialectInovance // AlignDefault

	done := make(chan readListResult, 1)
	go func() {
		vals, err := client.readList(context.Background(), []tagDesc{{
			TagName:  "application__gvl__Stru.flags",
			TagType:  CIPTypeBOOL,
			Elements: 3,
		}})
		done <- readListResult{vals, err}
	}()

	req := fs.awaitRequest(time.Second)
	// true, false, true —— 每个元素 2 字节
	fs.replyMultiRead(req.seq, [][]byte{
		multiReadReplyBody(CIPTypeBOOL, 0x00, []byte{0x01, 0x00, 0x00, 0x00, 0x01, 0x00}),
	})

	r := <-done
	if r.err != nil {
		t.Fatalf("readList 返回错误: %v", r.err)
	}
	got, ok := r.vals[0].([]any)
	if !ok || len(got) != 3 {
		t.Fatalf("结果形态不对: %#v", r.vals)
	}
	want := []bool{true, false, true}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("元素 %d = %v, 期望 %v（完整: %v）", i, got[i], w, got)
		}
	}
}

// 顶层 BOOL[n] 逐元素读：每元素 1 字节，不能跳 padding（文档 §4.2）。
func TestReadListInovanceTopLevelBoolHasNoPadding(t *testing.T) {
	client, fs := newFakeCIPClient(t)
	client.Dialect = DialectInovance

	done := make(chan readListResult, 1)
	go func() {
		vals, err := client.readList(context.Background(), []tagDesc{{
			TagName:  "application__gvl__MyBoolArray",
			TagType:  CIPTypeBOOL,
			Elements: 3,
		}})
		done <- readListResult{vals, err}
	}()

	req := fs.awaitRequest(time.Second)
	fs.replyMultiRead(req.seq, [][]byte{
		multiReadReplyBody(CIPTypeBOOL, 0x00, []byte{0x01, 0x00, 0x01}),
	})

	r := <-done
	if r.err != nil {
		t.Fatalf("readList 返回错误: %v", r.err)
	}
	got := r.vals[0].([]any)
	want := []bool{true, false, true}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("元素 %d = %v, 期望 %v（完整: %v）", i, got[i], w, got)
		}
	}
}

// 响应里少了 padding 字节时必须报错，不能静默错位。
func TestReadListInovanceBoolPaddingMissingIsReported(t *testing.T) {
	client, fs := newFakeCIPClient(t)
	client.Dialect = DialectInovance

	done := make(chan readListResult, 1)
	go func() {
		vals, err := client.readList(context.Background(), []tagDesc{{
			TagName:  "Stru.flags",
			TagType:  CIPTypeBOOL,
			Elements: 3,
		}})
		done <- readListResult{vals, err}
	}()

	req := fs.awaitRequest(time.Second)
	// 3 字节给 3 个"2 字节成员"——第二个元素之后就没有 padding 了
	fs.replyMultiRead(req.seq, [][]byte{
		multiReadReplyBody(CIPTypeBOOL, 0x00, []byte{0x01, 0x00, 0x01}),
	})

	r := <-done
	if r.err == nil {
		t.Fatalf("期望报错，实际返回 %v", r.vals)
	}
	if !strings.Contains(r.err.Error(), "padding") {
		t.Fatalf("错误信息里应说明 padding 缺失: %v", r.err)
	}
}

// 单标签多元素读（ReadSingleWithContext）走同一条规则。
func TestReadSingleInovanceBoolMemberSkipsPadding(t *testing.T) {
	client, fs := newFakeCIPClient(t)
	client.Dialect = DialectInovance

	done := make(chan struct {
		val any
		err error
	}, 1)
	go func() {
		v, err := client.ReadSingleWithContext(context.Background(), "Stru.flags", CIPTypeBOOL, 3)
		done <- struct {
			val any
			err error
		}{v, err}
	}()

	req := fs.awaitRequest(time.Second)
	fs.replyConnectedRead(req.service, req.seq, 0x00, CIPTypeBOOL, []byte{0x01, 0x00, 0x00, 0x00, 0x01, 0x00})

	r := <-done
	if r.err != nil {
		t.Fatalf("ReadSingleWithContext 返回错误: %v", r.err)
	}
	got, ok := r.val.([]any)
	if !ok || len(got) != 3 {
		t.Fatalf("结果形态不对: %#v", r.val)
	}
	want := []bool{true, false, true}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("元素 %d = %v, 期望 %v（完整: %v）", i, got[i], w, got)
		}
	}
}

// Logix 方言不受影响：BOOL 元素仍是 1 字节、不跳 padding。
func TestReadListLogixBoolUnchanged(t *testing.T) {
	client, fs := newFakeCIPClient(t)

	done := make(chan readListResult, 1)
	go func() {
		vals, err := client.readList(context.Background(), []tagDesc{{
			TagName:  "Stru.flags",
			TagType:  CIPTypeBOOL,
			Elements: 3,
		}})
		done <- readListResult{vals, err}
	}()

	req := fs.awaitRequest(time.Second)
	fs.replyMultiRead(req.seq, [][]byte{
		multiReadReplyBody(CIPTypeBOOL, 0x00, []byte{0x01, 0x00, 0x01}),
	})

	r := <-done
	if r.err != nil {
		t.Fatalf("readList 返回错误: %v", r.err)
	}
	got := r.vals[0].([]any)
	if got[0] != true || got[1] != false || got[2] != true {
		t.Fatalf("Logix 结果不对: %v", got)
	}
}
