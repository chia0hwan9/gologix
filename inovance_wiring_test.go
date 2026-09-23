package gologix

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestInvIsMemberTag(t *testing.T) {
	tests := []struct {
		tag  string
		want bool
	}{
		{tag: "MyBool", want: false},
		{tag: "application__gvl__taga", want: false},
		{tag: "application__gvl__Stru.m0", want: true},
		{tag: "Stru.m1[3]", want: true},
	}
	for _, tt := range tests {
		if got := invIsMemberTag(tt.tag); got != tt.want {
			t.Errorf("invIsMemberTag(%q) = %v, want %v", tt.tag, got, tt.want)
		}
	}
}

func newInovanceClient(align InovanceAlign) *Client {
	c := NewClient("127.0.0.1")
	c.Dialect = DialectInovance
	c.Inovance.Align = align
	return c
}

// The BOOL payload rules: top level BOOL is 1 byte and BOOL[n] is n bytes
// (§4.2); a BOOL member is 2 bytes under the default alignment (§4.4.2(1)) and
// a BOOL[n] member is bit packed and rounded up to 16 bits (§4.4.2(2)).
// Alignment parameter 1 (InoProShop) makes everything one byte per element.
func TestInovanceBoolWritePayloads(t *testing.T) {
	tests := []struct {
		name  string
		align InovanceAlign
		tag   string
		value any
		want  []byte
	}{
		{
			name:  "top level bool",
			align: AlignDefault,
			tag:   "MyBool",
			value: true,
			want:  []byte{0x01},
		},
		{
			name:  "top level bool false",
			align: AlignDefault,
			tag:   "MyBool",
			value: false,
			want:  []byte{0x00},
		},
		{
			name:  "bool member default alignment is 2 bytes",
			align: AlignDefault,
			tag:   "application__gvl__Stru.m0",
			value: true,
			want:  []byte{0x01, 0x00},
		},
		{
			name:  "bool member false default alignment is 2 bytes",
			align: AlignDefault,
			tag:   "Stru.m0",
			value: false,
			want:  []byte{0x00, 0x00},
		},
		{
			name:  "bool member with alignment 1 is 1 byte",
			align: AlignInoProShop,
			tag:   "Stru.m0",
			value: true,
			want:  []byte{0x01},
		},
		{
			name:  "top level bool array is one byte per element",
			align: AlignDefault,
			tag:   "MyBoolArray",
			value: []bool{true, false, true},
			want:  []byte{0x01, 0x00, 0x01},
		},
		{
			name:  "bool array member default alignment is bit packed",
			align: AlignDefault,
			tag:   "Stru.m1",
			value: []bool{true, false, true},
			// 3 bits round up to 16 bits = 2 bytes
			want: []byte{0x05, 0x00},
		},
		{
			name:  "bool array member default alignment crosses bytes",
			align: AlignDefault,
			tag:   "Stru.m1",
			value: func() []bool {
				v := make([]bool, 10)
				v[0] = true
				v[9] = true
				return v
			}(),
			// Bool[10] = 2 bytes, bit0 and bit9
			want: []byte{0x01, 0x02},
		},
		{
			name:  "bool array member with alignment 1 is one byte per element",
			align: AlignInoProShop,
			tag:   "Stru.m1",
			value: []bool{true, false, true},
			want:  []byte{0x01, 0x00, 0x01},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newInovanceClient(tt.align)
			item := CIPItem{}
			if err := c.invSerializeValue(&item, tt.tag, tt.value, invTypeBOOL); err != nil {
				t.Fatalf("invSerializeValue: %v", err)
			}
			if !bytes.Equal(item.Data, tt.want) {
				t.Fatalf("payload mismatch\n have % X\n want % X", item.Data, tt.want)
			}
			if int(item.Header.Length) != len(tt.want) {
				t.Fatalf("item header length = %d, want %d", item.Header.Length, len(tt.want))
			}
		})
	}
}

func TestInovanceSerializeUnsupportedTypes(t *testing.T) {
	c := newInovanceClient(AlignDefault)

	item := CIPItem{}
	err := c.invSerializeValue(&item, "MyString", "abc", invTypeSTRING)
	if err == nil {
		t.Fatal("string write: expected an error")
	}
	if !strings.Contains(err.Error(), "P1") {
		t.Errorf("string write: unexpected error %v", err)
	}

	item = CIPItem{}
	err = c.invSerializeValue(&item, "MyStr", invUDT1{}, invTypeSTRUCT)
	if err == nil {
		t.Fatal("struct write: expected an error")
	}
	if !strings.Contains(err.Error(), "UDT write path") {
		t.Errorf("struct write: unexpected error %v", err)
	}

	// atomic arrays keep the plain little endian layout
	item = CIPItem{}
	if err := c.invSerializeValue(&item, "MyInts", []int16{0x1234, -1}, invTypeINT); err != nil {
		t.Fatalf("atomic array: %v", err)
	}
	if want := []byte{0x34, 0x12, 0xFF, 0xFF}; !bytes.Equal(item.Data, want) {
		t.Fatalf("atomic array mismatch: have % X want % X", item.Data, want)
	}
}

func TestInovanceUnpackBoolWords(t *testing.T) {
	data := make([]bool, 17)

	// bit0 of the first word and bit0 of the second word (= bit 16)
	invUnpackBoolWords([]uint16{0x0001, 0x0001}, data)
	for i, v := range data {
		want := i == 0 || i == 16
		if v != want {
			t.Errorf("data[%d] = %v, want %v", i, v, want)
		}
	}

	// padding bits past the end of data must be ignored
	data = make([]bool, 17)
	invUnpackBoolWords([]uint16{0x0000, 0x8000}, data) // bit15 of word 1 = bit 31
	for i, v := range data {
		if v {
			t.Errorf("padding bit leaked into data[%d]", i)
		}
	}
}

// A struct write on an Inovance client must fail loudly rather than send a
// Logix UDT frame (type encoding CRC + 0xA0).
func TestInovanceStructWriteIsRejected(t *testing.T) {
	c := newInovanceClient(AlignDefault)
	err := c.write_udt(context.TODO(), "Stru", invUDT1{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "not implemented") {
		t.Errorf("unexpected error %v", err)
	}

	// Logix clients are unaffected
	l := NewClient("127.0.0.1")
	if l.isInovance() {
		t.Error("logix client reported as inovance")
	}
}
