package gologix

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

// replyConnectedError sends a well-formed CIP *error* reply: sequence counter,
// service, reserved, general status and an additional-status word count of 0,
// with no data type code and no data. This is exactly what a controller sends
// when the requested tag does not exist.
func (fs *fakeCIPServer) replyConnectedError(svc CIPService, seq uint16, status byte) {
	fs.t.Helper()
	fs.sendFrame(cipCommandSendUnitData, buildConnectedReply(seq, 0x00112233, svc, status, nil))
}

// 读不存在的 tag：PLC 返回的是合法应答，只是"短"（没有类型头和数据体）。
// 修复前它会被反序列化成 io.ErrUnexpectedEOF，看起来像连接断了。
func TestReadUnknownTagReturnsTypedCIPError(t *testing.T) {
	client, fs := newFakeCIPClient(t)

	done := make(chan error, 1)
	go func() {
		var v bool
		done <- client.Read("Tag_That_Does_Not_Exist", &v)
	}()

	req := fs.awaitRequest(time.Second)
	fs.replyConnectedError(req.service, req.seq, byte(CIPStatus_PathDestinationUnknown))

	err := <-done
	if err == nil {
		t.Fatal("读不存在的 tag 应当报错，实际成功")
	}
	if !errors.Is(err, ErrCIPPathNotFound) || !IsTagNotFound(err) {
		t.Errorf("错误应可被 errors.Is(ErrCIPPathNotFound)/IsTagNotFound 识别: %v", err)
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("不应再伪装成传输层短读: %v", err)
	}

	var cipErr *CIPError
	if !errors.As(err, &cipErr) {
		t.Fatalf("应能取出 *CIPError: %v", err)
	}
	if cipErr.Code != byte(CIPStatus_PathDestinationUnknown) {
		t.Errorf("Code = 0x%02X, want 0x05", cipErr.Code)
	}
}

// 收到 CIP 错误应答后会话必须仍然可用：同一个 client 紧接着读一个存在的 tag 要成功，
// 不能触发重连。
func TestReadAfterCIPErrorKeepsSessionUsable(t *testing.T) {
	client, fs := newFakeCIPClient(t)

	done := make(chan error, 1)
	go func() {
		var v bool
		done <- client.Read("MissingTag", &v)
	}()
	req := fs.awaitRequest(time.Second)
	fs.replyConnectedError(req.service, req.seq, byte(CIPStatus_PathDestinationUnknown))
	if err := <-done; !IsTagNotFound(err) {
		t.Fatalf("第一次读应报 tag 不存在，实际 %v", err)
	}

	done2 := make(chan error, 1)
	go func() {
		var v bool
		done2 <- client.Read("KnownGoodBool", &v)
	}()
	req2 := fs.awaitRequest(time.Second)
	fs.replyConnectedRead(req2.service, req2.seq, 0x00, CIPTypeBOOL, []byte{0x01})
	if err := <-done2; err != nil {
		t.Fatalf("CIP 错误应答后同一连接应仍可用，实际 %v", err)
	}
}

// 真正的短读（成功状态却没有类型头/数据）仍必须报传输层错误，
// 不能被误判成 CIP 错误应答。
func TestTruncatedSuccessReplyStillReportsShortRead(t *testing.T) {
	client, fs := newFakeCIPClient(t)

	done := make(chan error, 1)
	go func() {
		var v bool
		done <- client.Read("SomeTag", &v)
	}()

	req := fs.awaitRequest(time.Second)
	// status = 0x00 但连类型头都没给全（只有 6 字节头 + 1 字节）
	fs.sendFrame(cipCommandSendUnitData, buildConnectedReply(req.seq, 0x00112233, req.service, 0x00, []byte{0x01}))

	err := <-done
	if err == nil {
		t.Fatal("期望报错")
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("截断的成功应答应报短读（io.ErrUnexpectedEOF），实际 %v", err)
	}
	if IsTagNotFound(err) {
		t.Errorf("截断的成功应答不应被当成 tag 不存在: %v", err)
	}
}

func TestParseCIPErrorReply(t *testing.T) {
	// seq(2) + service(回应位) + reserved + general status + ext size
	reply := func(status byte) []byte {
		return []byte{0x34, 0x12, byte(CIPService_Read.AsResponse()), 0x00, status, 0x00}
	}

	t.Run("非零状态判为 CIP 错误", func(t *testing.T) {
		e, ok := parseCIPErrorReply(reply(byte(CIPStatus_PathDestinationUnknown)))
		if !ok || e == nil {
			t.Fatal("0x05 应判为 CIP 错误应答")
		}
		if e.Code != byte(CIPStatus_PathDestinationUnknown) {
			t.Errorf("Code = 0x%02X, want 0x05", e.Code)
		}
		if !errors.Is(e, ErrCIPPathNotFound) {
			t.Errorf("errors.Is(e, ErrCIPPathNotFound) 应为 true: %v", e)
		}
	})

	t.Run("带附加状态的错误应答同样判为 CIP 错误，并解出 Extended", func(t *testing.T) {
		// 比 8 字节长，旧代码会"反序列化成功"然后把附加状态当类型码
		b := []byte{0x34, 0x12, 0xCC, 0x00, byte(CIPStatus_VendorSpecificError), 0x02, 0x07, 0x80, 0x00, 0x00}
		e, ok := parseCIPErrorReply(b)
		if !ok || e.Code != byte(CIPStatus_VendorSpecificError) {
			t.Fatalf("0x1F 应判为 CIP 错误应答: ok=%v e=%v", ok, e)
		}
		if e.Extended != 0x8007 {
			t.Errorf("Extended = 0x%04X, want 0x8007（第一个附加状态字）", e.Extended)
		}
		if sentinelForStatus(CIPStatus(e.Code)) != nil {
			t.Error("0x1F 没有对应 sentinel，应为 nil")
		}
	})

	t.Run("成功与 PartialTransfer 必须放行", func(t *testing.T) {
		if _, ok := parseCIPErrorReply(reply(byte(CIPStatus_OK))); ok {
			t.Error("status 0x00 不应判为错误应答")
		}
		if _, ok := parseCIPErrorReply(reply(byte(CIPStatus_PartialTransfer))); ok {
			t.Error("status 0x06 必须放行，否则会打断分片读")
		}
	})

	t.Run("没有回应位的缓冲不碰", func(t *testing.T) {
		b := []byte{0x34, 0x12, 0x4C, 0x00, 0x05, 0x00} // 服务码无 0x80
		if _, ok := parseCIPErrorReply(b); ok {
			t.Error("无回应位不应判为 CIP 错误应答")
		}
	})

	t.Run("过短的缓冲不判", func(t *testing.T) {
		if _, ok := parseCIPErrorReply([]byte{0x34, 0x12, 0xCC, 0x00}); ok {
			t.Error("不足 5 字节不应判为错误应答")
		}
	})
}

func TestCIPErrorSentinelMapping(t *testing.T) {
	tests := []struct {
		status CIPStatus
		want   error
	}{
		{CIPStatus_PathDestinationUnknown, ErrCIPPathNotFound},
		{CIPStatus_ServiceNotSupported, ErrCIPServiceNotSupported},
		{CIPStatus_NotEnoughData, ErrCIPNotEnoughData},
		{CIPStatus_ObjectStateConflict, nil},
	}
	for _, tt := range tests {
		err := &CIPError{Code: byte(tt.status)}
		if got := sentinelForStatus(tt.status); got != tt.want {
			t.Errorf("sentinelForStatus(%v) = %v, want %v", tt.status, got, tt.want)
		}
		if tt.want != nil && !errors.Is(err, tt.want) {
			t.Errorf("errors.Is(CIPError{%v}, %v) 应为 true", tt.status, tt.want)
		}
		if tt.want == nil && !errors.As(error(err), new(*CIPError)) {
			t.Errorf("无 sentinel 时仍应能 errors.As 出 *CIPError")
		}
	}
}

// 保证修复不影响正常读路径（顺带回归分片读的入口条件）。
func TestNormalReadStillWorks(t *testing.T) {
	client, fs := newFakeCIPClient(t)

	done := make(chan struct {
		v   any
		err error
	}, 1)
	go func() {
		v, err := client.ReadSingleWithContext(context.Background(), "KnownGoodDint", CIPTypeDINT, 1)
		done <- struct {
			v   any
			err error
		}{v, err}
	}()

	req := fs.awaitRequest(time.Second)
	fs.replyConnectedRead(req.service, req.seq, 0x00, CIPTypeDINT, []byte{0x2A, 0x00, 0x00, 0x00})

	r := <-done
	if r.err != nil {
		t.Fatalf("正常读不应报错: %v", r.err)
	}
	if v, ok := r.v.(int32); !ok || v != 42 {
		t.Fatalf("读到的值不对: %#v", r.v)
	}
}

// multiReadErrorBody 是多服务应答里"某个 item 失败"的 body：
// service(1) + reserved(1) + general status(1) + ext size(1)，没有类型头和数据。
func multiReadErrorBody(status byte) []byte {
	return []byte{byte(CIPService_Read.AsResponse()), 0x00, status, 0x00}
}

// 批量读（多服务）里某个 tag 不存在时，PLC 的顶层状态是成功、只有该 item 是
// 4 字节的错误应答。按成功的 6 字节结构去读会短读成 io.ErrUnexpectedEOF，
// 于是同样会被误判成连接失效。
func TestReadListUnknownTagReturnsTypedCIPError(t *testing.T) {
	client, fs := newFakeCIPClient(t)

	done := make(chan error, 1)
	go func() {
		_, err := client.readList(context.Background(), []tagDesc{
			{TagName: "GoodTag", TagType: CIPTypeDINT, Elements: 1},
			{TagName: "MissingTag", TagType: CIPTypeDINT, Elements: 1},
		})
		done <- err
	}()

	req := fs.awaitRequest(time.Second)
	fs.replyMultiRead(req.seq, [][]byte{
		multiReadReplyBody(CIPTypeDINT, 0, []byte{0x2A, 0x00, 0x00, 0x00}),
		multiReadErrorBody(byte(CIPStatus_PathDestinationUnknown)),
	})

	err := <-done
	if err == nil {
		t.Fatal("批量读里出现不存在的 tag 应当报错")
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("不应伪装成传输层短读（会被判连接失效）: %v", err)
	}
	if !IsTagNotFound(err) {
		t.Errorf("应能识别 tag 不存在: %v", err)
	}
}

// 顶层状态非 0（PLC 直接拒掉整个多服务请求）也应给出类型化错误。
func TestReadListTopLevelCIPErrorIsTyped(t *testing.T) {
	client, fs := newFakeCIPClient(t)

	done := make(chan error, 1)
	go func() {
		_, err := client.readList(context.Background(), []tagDesc{
			{TagName: "MissingTag", TagType: CIPTypeDINT, Elements: 1},
		})
		done <- err
	}()

	req := fs.awaitRequest(time.Second)
	fs.sendFrame(cipCommandSendUnitData,
		buildConnectedReply(req.seq, 0x00112233, CIPService_MultipleService, byte(CIPStatus_PathDestinationUnknown), nil))

	err := <-done
	if err == nil {
		t.Fatal("顶层错误状态应当报错")
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("不应伪装成传输层短读: %v", err)
	}
	if !IsTagNotFound(err) {
		t.Errorf("应能识别 tag 不存在: %v", err)
	}
}
