package gologix

import (
	"bytes"
	"context"
	"encoding/binary"
	"strings"
	"testing"
	"time"
)

// replyConnectedWrite sends a successful write reply: service, reserved, status
// zero and an extended status size of zero, with no payload.
func (fs *fakeCIPServer) replyConnectedWrite(svc CIPService, seq uint16) {
	fs.t.Helper()
	fs.sendFrame(cipCommandSendUnitData, buildConnectedReply(seq, 0x00112233, svc, 0x00, nil))
}

// omronWriteRequest is the parsed body of a write request: path, data type,
// element count and the value bytes.
type omronWriteRequest struct {
	path     []byte
	dataType uint16
	elements uint16
	value    []byte
}

// parseOmronWriteRequest parses the connected-data item of a write request.
func parseOmronWriteRequest(t *testing.T, req fakeRequest) omronWriteRequest {
	t.Helper()
	r := bytes.NewReader(req.rawPayload)
	var pre msgPreItemData
	if err := binary.Read(r, binary.LittleEndian, &pre); err != nil {
		t.Fatalf("pre item header: %v", err)
	}
	items, err := readItems(r)
	if err != nil {
		t.Fatalf("read items: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	body := items[1].Data
	// seq(2) + service(1) + path size(1) + path + dataType(2) + elements(2) + value
	if len(body) < 6 {
		t.Fatalf("write body too short: %d bytes", len(body))
	}
	pos := 2 + 1
	pathWords := int(body[pos])
	pos++
	path := body[pos : pos+pathWords*2]
	pos += pathWords * 2
	out := omronWriteRequest{path: path}
	out.dataType = binary.LittleEndian.Uint16(body[pos:])
	pos += 2
	out.elements = binary.LittleEndian.Uint16(body[pos:])
	pos += 2
	out.value = body[pos:]
	return out
}

func TestUseOmronSetsConnectionDefaults(t *testing.T) {
	client := NewClient("192.168.250.1")
	if err := client.UseOmron(); err != nil {
		t.Fatalf("UseOmron: %v", err)
	}
	if client.Dialect != DialectOmron {
		t.Errorf("Dialect = %v, want omron", client.Dialect)
	}
	if client.Controller.Path == nil || client.Controller.Path.Len() != 0 {
		t.Errorf("Controller.Path must be empty for NJ/NX (no backplane), got %v bytes", client.Controller.Path.Len())
	}
	if client.ConnectionSize != omronConnectionSize {
		t.Errorf("ConnectionSize = %d, want %d", client.ConnectionSize, omronConnectionSize)
	}
	if !client.keepTagCase() {
		t.Error("Omron must keep the tag name as written")
	}
}

func TestUseOmronWithConnectionSize(t *testing.T) {
	client := NewClient("192.168.250.1")
	if err := client.UseOmronWithOptions(OmronOptions{ConnectionSize: 8192}); err != nil {
		t.Fatalf("UseOmronWithOptions: %v", err)
	}
	if client.ConnectionSize != 8192 {
		t.Errorf("ConnectionSize = %d, want 8192", client.ConnectionSize)
	}
}

// The Forward_Open connection path must address the message router directly:
// 20 02 24 01, with no backplane/port segment in front of it.
func TestOmronForwardOpenPathIsMessageRouter(t *testing.T) {
	client := NewClient("192.168.250.1")
	if err := client.UseOmron(); err != nil {
		t.Fatalf("UseOmron: %v", err)
	}
	client.ConnectionSize = omronConnectionSize

	item, err := client.newForwardOpenLarge()
	if err != nil {
		t.Fatalf("newForwardOpenLarge: %v", err)
	}
	want := []byte{0x20, 0x02, 0x24, 0x01}
	if !bytes.HasSuffix(item.Data, want) {
		t.Errorf("connection path must end with % X, got tail % X", want, item.Data[len(item.Data)-8:])
	}
	if !bytes.Contains(item.Data, []byte{0xCA, 0x07}) {
		t.Errorf("expected the 1994 byte connection size (CA 07) in the Forward_Open parameters")
	}
}

func TestOmronWriteBoolSendsTwoBytes(t *testing.T) {
	client, fs := newFakeCIPClient(t)
	if err := client.UseOmron(); err != nil {
		t.Fatalf("UseOmron: %v", err)
	}

	for _, tc := range []struct {
		value bool
		want  []byte
	}{
		{true, []byte{0x01, 0x00}},
		{false, []byte{0x00, 0x00}},
	} {
		done := make(chan error, 1)
		go func() {
			done <- client.Write("MyBool", tc.value)
		}()
		req := fs.awaitRequest(time.Second)
		got := parseOmronWriteRequest(t, req)
		if got.dataType != uint16(CIPTypeBOOL) {
			t.Errorf("value %v: data type = 0x%02X, want 0x%02X", tc.value, got.dataType, byte(CIPTypeBOOL))
		}
		if got.elements != 1 {
			t.Errorf("value %v: elements = %d, want 1", tc.value, got.elements)
		}
		if !bytes.Equal(got.value, tc.want) {
			t.Errorf("value %v: payload = % X, want % X", tc.value, got.value, tc.want)
		}
		fs.replyConnectedWrite(req.service, req.seq)
		if err := <-done; err != nil {
			t.Fatalf("write %v: %v", tc.value, err)
		}
	}
}

// The tag name must reach the wire exactly as written (Sysmac Studio names are
// case sensitive).
func TestOmronKeepsTagCase(t *testing.T) {
	client, fs := newFakeCIPClient(t)
	if err := client.UseOmron(); err != nil {
		t.Fatalf("UseOmron: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- client.Write("MyMixedCaseTag", true)
	}()
	req := fs.awaitRequest(time.Second)
	got := parseOmronWriteRequest(t, req)
	if !bytes.Contains(got.path, []byte("MyMixedCaseTag")) {
		t.Errorf("path % X does not contain the original spelling MyMixedCaseTag", got.path)
	}
	fs.replyConnectedWrite(req.service, req.seq)
	if err := <-done; err != nil {
		t.Fatalf("write: %v", err)
	}
}

// A single BOOL read reply carries two bytes; the value comes from the status
// byte and the second byte must not desynchronise the stream.
func TestOmronReadBoolReadsStatusByte(t *testing.T) {
	client, fs := newFakeCIPClient(t)
	if err := client.UseOmron(); err != nil {
		t.Fatalf("UseOmron: %v", err)
	}

	var got bool
	done := make(chan error, 1)
	go func() {
		done <- client.Read("MyBool", &got)
	}()
	req := fs.awaitRequest(time.Second)
	fs.replyConnectedRead(req.service, req.seq, 0x00, CIPTypeBOOL, []byte{0x01, 0x00})
	if err := <-done; err != nil {
		t.Fatalf("read: %v", err)
	}
	if !got {
		t.Error("expected true")
	}

	// The next read must still line up.
	var dint int32
	done2 := make(chan error, 1)
	go func() {
		done2 <- client.Read("MyDint", &dint)
	}()
	req2 := fs.awaitRequest(time.Second)
	fs.replyConnectedRead(req2.service, req2.seq, 0x00, CIPTypeDINT, []byte{0x2A, 0x00, 0x00, 0x00})
	if err := <-done2; err != nil {
		t.Fatalf("second read: %v", err)
	}
	if dint != 42 {
		t.Errorf("dint = %d, want 42", dint)
	}
}

// A whole BOOL array can come back in three shapes; the decoder picks the one
// that matches the reply length.
func TestOmronReadBoolArrayLayouts(t *testing.T) {
	cases := []struct {
		name  string
		n     int
		reply []byte
		want  []bool
	}{
		{
			name:  "bit array 16 bits per word",
			n:     32,
			reply: []byte{0x01, 0x00, 0x01, 0x00}, // bit 0 of word 0 and word 1
			want:  []bool{true, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, true},
		},
		{
			name:  "one status byte per element",
			n:     4,
			reply: []byte{0x01, 0x00, 0x01, 0x00},
			want:  []bool{true, false, true, false},
		},
		{
			name:  "two bytes per element",
			n:     2,
			reply: []byte{0x01, 0x00, 0x00, 0x00},
			want:  []bool{true, false},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, fs := newFakeCIPClient(t)
			if err := client.UseOmron(); err != nil {
				t.Fatalf("UseOmron: %v", err)
			}
			bools := make([]bool, tc.n)
			done := make(chan error, 1)
			go func() {
				done <- client.Read("MyBools", bools)
			}()
			req := fs.awaitRequest(time.Second)
			if req.elements != uint16(tc.n) {
				t.Errorf("elements = %d, want %d", req.elements, tc.n)
			}
			fs.replyConnectedRead(req.service, req.seq, 0x00, CIPTypeBOOL, tc.reply)
			if err := <-done; err != nil {
				t.Fatalf("Read: %v", err)
			}
			for i, want := range tc.want {
				if bools[i] != want {
					t.Errorf("element %d = %v, want %v", i, bools[i], want)
				}
			}
		})
	}
}

func TestOmronReadBoolArrayUnknownLengthIsRejected(t *testing.T) {
	client, fs := newFakeCIPClient(t)
	if err := client.UseOmron(); err != nil {
		t.Fatalf("UseOmron: %v", err)
	}
	bools := make([]bool, 3)
	done := make(chan error, 1)
	go func() {
		done <- client.Read("MyBools", bools)
	}()
	req := fs.awaitRequest(time.Second)
	fs.replyConnectedRead(req.service, req.seq, 0x00, CIPTypeBOOL, []byte{1, 0, 1, 0, 1})
	err := <-done
	if err == nil || !strings.Contains(err.Error(), "none of the known layouts") {
		t.Errorf("unexpected reply length should be reported, got %v", err)
	}
}

// A whole BOOL array is written as the vendor bit array type with 16 bit/WORD
// packing.
func TestOmronWriteBoolArrayUsesBitArrayType(t *testing.T) {
	client, fs := newFakeCIPClient(t)
	if err := client.UseOmron(); err != nil {
		t.Fatalf("UseOmron: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- client.Write("MyBools", []bool{true, false, false, true})
	}()
	req := fs.awaitRequest(time.Second)
	got := parseOmronWriteRequest(t, req)
	if got.dataType != uint16(omronTypeBitArray) {
		t.Errorf("data type = 0x%02X, want 0x%02X", got.dataType, byte(omronTypeBitArray))
	}
	if got.elements != 4 {
		t.Errorf("elements = %d, want 4", got.elements)
	}
	if want := []byte{0x09, 0x00}; !bytes.Equal(got.value, want) {
		t.Errorf("payload = % X, want % X", got.value, want)
	}
	fs.replyConnectedWrite(req.service, req.seq)
	if err := <-done; err != nil {
		t.Fatalf("Write: %v", err)
	}
}

// The batch (multiple service) path must decode a bit array response too: the
// controller reports it as type 0xFF, which is also this library's Logix
// STRING marker, so readList needs an explicit Omron branch.
func TestOmronReadListBoolArrayBitPacked(t *testing.T) {
	client, fs := newFakeCIPClient(t)
	if err := client.UseOmron(); err != nil {
		t.Fatalf("UseOmron: %v", err)
	}

	done := make(chan struct {
		vals []any
		err  error
	}, 1)
	go func() {
		vals, err := client.readList(context.Background(), []tagDesc{
			{TagName: "MyBools", TagType: CIPTypeBOOL, Elements: 32},
		})
		done <- struct {
			vals []any
			err  error
		}{vals, err}
	}()

	req := fs.awaitRequest(time.Second)
	// word 0 bit 0 and word 1 bit 0 set
	fs.replyMultiRead(req.seq, [][]byte{
		multiReadReplyBody(omronTypeBitArray, 0, []byte{0x01, 0x00, 0x01, 0x00}),
	})

	r := <-done
	if r.err != nil {
		t.Fatalf("readList: %v", r.err)
	}
	if len(r.vals) != 1 {
		t.Fatalf("got %d results, want 1", len(r.vals))
	}
	got, ok := r.vals[0].([]any)
	if !ok {
		t.Fatalf("result = %T, want []any", r.vals[0])
	}
	if len(got) != 32 {
		t.Fatalf("got %d bools, want 32", len(got))
	}
	for i, v := range got {
		b, ok := v.(bool)
		if !ok {
			t.Fatalf("element %d = %T, want bool", i, v)
		}
		want := i == 0 || i == 16
		if b != want {
			t.Errorf("element %d = %v, want %v", i, b, want)
		}
	}
}

func TestOmronStructWriteIsRejected(t *testing.T) {
	client, _ := newFakeCIPClient(t)
	if err := client.UseOmron(); err != nil {
		t.Fatalf("UseOmron: %v", err)
	}

	type myUDT struct {
		A int32
		B int16
	}
	err := client.Write("MyUDT", myUDT{A: 1, B: 2})
	if err == nil || !strings.Contains(err.Error(), "Omron struct writes are not implemented") {
		t.Errorf("struct write should be rejected with a clear error, got %v", err)
	}
}

// Reading a BOOL array element by element must still work: once a Num of
// Element is specified each element is one status byte (W506 §7-7-4).
func TestOmronElementWiseBoolArrayRead(t *testing.T) {
	client, fs := newFakeCIPClient(t)
	if err := client.UseOmron(); err != nil {
		t.Fatalf("UseOmron: %v", err)
	}

	done := make(chan struct {
		v   any
		err error
	}, 1)
	go func() {
		v, err := client.ReadSingleWithContext(context.Background(), "MyBools", CIPTypeBOOL, 3)
		done <- struct {
			v   any
			err error
		}{v, err}
	}()
	req := fs.awaitRequest(time.Second)
	if req.elements != 3 {
		t.Errorf("elements = %d, want 3", req.elements)
	}
	fs.replyConnectedRead(req.service, req.seq, 0x00, CIPTypeBOOL, []byte{0x01, 0x00, 0x01})

	r := <-done
	if r.err != nil {
		t.Fatalf("ReadSingleWithContext: %v", r.err)
	}
	// Multi-element reads come back as []any of the element type.
	got, ok := r.v.([]any)
	if !ok {
		t.Fatalf("value = %T, want []any", r.v)
	}
	want := []bool{true, false, true}
	if len(got) != len(want) {
		t.Fatalf("got %d elements, want %d", len(got), len(want))
	}
	for i := range want {
		b, ok := got[i].(bool)
		if !ok {
			t.Fatalf("element %d = %T, want bool", i, got[i])
		}
		if b != want[i] {
			t.Errorf("element %d = %v, want %v", i, b, want[i])
		}
	}
}

func TestDialectString(t *testing.T) {
	for _, tc := range []struct {
		d    Dialect
		want string
	}{
		{DialectLogix, "logix"},
		{DialectInovance, "inovance"},
		{DialectOmron, "omron"},
	} {
		if got := tc.d.String(); got != tc.want {
			t.Errorf("Dialect(%d).String() = %q, want %q", int(tc.d), got, tc.want)
		}
	}
}
