package gologix

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

// The byte level expectations in this file come from the two layout tables on
// pages 16/17 of 汇川《EIP 标签通信库使用说明》V2.0.2.8:
//
//	UDT1 { m0:INT; m1:DINT }
//	UDT2 { m0:BOOL; m1:BOOL[10]; m2:DINT }
//
// AT_DEFAULT:      UDT1 = 8 bytes (m0@0..1, pad@2..3, m1@4..7)
//                  UDT2 = 8 bytes (m0 = byte0 bit0, byte1 empty,
//                                  m1 bits 0..9 = byte2 bit0..7 + byte3 bit0..1,
//                                  m2@4..7)
// AT_INOPROSHOP(1):UDT1 = 6 bytes (m0@0..1, m1@2..5)
//                  UDT2 = 15 bytes (m0@0, m1[0..9]@1..10, m2@11..14)

type invUDT1 struct {
	M0 int16
	M1 int32
}

type invUDT2 struct {
	M0 bool
	M1 [10]bool
	M2 int32
}

func TestInovanceLayoutDocFixtures(t *testing.T) {
	tests := []struct {
		name string
		mode InovanceAlign
		in   any
		want []byte
	}{
		{
			name: "UDT1 default alignment",
			mode: AlignDefault,
			in:   invUDT1{M0: 0x1234, M1: 0x11223344},
			want: []byte{0x34, 0x12, 0x00, 0x00, 0x44, 0x33, 0x22, 0x11},
		},
		{
			name: "UDT1 inoproshop alignment",
			mode: AlignInoProShop,
			in:   invUDT1{M0: 0x1234, M1: 0x11223344},
			want: []byte{0x34, 0x12, 0x44, 0x33, 0x22, 0x11},
		},
		{
			name: "UDT2 default alignment",
			mode: AlignDefault,
			in: func() invUDT2 {
				v := invUDT2{M0: true, M2: 0x0A0B0C0D}
				v.M1[0] = true
				v.M1[8] = true
				v.M1[9] = true
				return v
			}(),
			//            m0 bit0   m0 2nd byte empty   m1[0..7]  m1[8],m1[9]  m2 (LE)
			want: []byte{0x01, 0x00, 0x01, 0x03, 0x0D, 0x0C, 0x0B, 0x0A},
		},
		{
			name: "UDT2 inoproshop alignment",
			mode: AlignInoProShop,
			in: func() invUDT2 {
				v := invUDT2{M0: true, M2: 0x0A0B0C0D}
				v.M1[0] = true
				v.M1[9] = true
				return v
			}(),
			want: []byte{
				0x01,                                                       // m0
				0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, // m1[0..9], one byte each
				0x0D, 0x0C, 0x0B, 0x0A, // m2, not 4 byte aligned
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			n, err := PackInovance(buf, tt.in, tt.mode)
			if err != nil {
				t.Fatalf("PackInovance: %v", err)
			}
			if n != len(tt.want) {
				t.Fatalf("packed %d bytes, want %d", n, len(tt.want))
			}
			if !bytes.Equal(buf.Bytes(), tt.want) {
				t.Fatalf("packed bytes mismatch\n have % X\n want % X", buf.Bytes(), tt.want)
			}

			size, err := InovanceStructSize(tt.in, tt.mode)
			if err != nil {
				t.Fatalf("InovanceStructSize: %v", err)
			}
			if size != len(tt.want) {
				t.Fatalf("InovanceStructSize = %d, want %d", size, len(tt.want))
			}

			// round trip
			out := reflect.New(reflect.TypeOf(tt.in)).Interface()
			rn, err := UnpackInovance(bytes.NewReader(tt.want), out, tt.mode)
			if err != nil {
				t.Fatalf("UnpackInovance: %v", err)
			}
			if rn != len(tt.want) {
				t.Fatalf("unpacked %d bytes, want %d", rn, len(tt.want))
			}
			got := reflect.ValueOf(out).Elem().Interface()
			if !reflect.DeepEqual(got, tt.in) {
				t.Fatalf("round trip mismatch\n have %+v\n want %+v", got, tt.in)
			}
		})
	}
}

// Bool[n] member sizes: 16-bit rounding under the default alignment, one byte
// per element under the InoProShop (align 1) rule.
func TestInovanceBoolArraySizes(t *testing.T) {
	tests := []struct {
		n   int
		def int
		ino int
	}{
		{n: 1, def: 2, ino: 1},
		{n: 8, def: 2, ino: 8},
		{n: 9, def: 2, ino: 9},
		{n: 10, def: 2, ino: 10},
		{n: 16, def: 2, ino: 16},
		{n: 17, def: 4, ino: 17},
		{n: 24, def: 4, ino: 24},
		{n: 32, def: 4, ino: 32},
		{n: 33, def: 6, ino: 33},
	}

	for _, tt := range tests {
		t.Run("n_"+itoa(tt.n), func(t *testing.T) {
			typ := reflect.StructOf([]reflect.StructField{{
				Name: "M",
				Type: reflect.ArrayOf(tt.n, reflect.TypeOf(true)),
			}})

			layout, err := invLayoutOfStructType(typ, AlignDefault)
			if err != nil {
				t.Fatalf("layout: %v", err)
			}
			if layout.size != tt.def {
				t.Errorf("default alignment: BOOL[%d] struct size = %d, want %d", tt.n, layout.size, tt.def)
			}
			layout, err = invLayoutOfStructType(typ, AlignInoProShop)
			if err != nil {
				t.Fatalf("layout: %v", err)
			}
			if layout.size != tt.ino {
				t.Errorf("inoproshop alignment: BOOL[%d] struct size = %d, want %d", tt.n, layout.size, tt.ino)
			}
		})
	}
}

func TestInovanceBoolArrayBitOrder(t *testing.T) {
	type s struct {
		M [20]bool
	}
	v := s{}
	for _, i := range []int{0, 7, 8, 16, 19} {
		v.M[i] = true
	}
	buf := &bytes.Buffer{}
	if _, err := PackInovance(buf, v, AlignDefault); err != nil {
		t.Fatalf("PackInovance: %v", err)
	}
	// BOOL[20] -> roundUp(20,16)/8 = 4 bytes
	want := []byte{0x81, 0x01, 0x09, 0x00}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("bit order mismatch\n have % X\n want % X", buf.Bytes(), want)
	}

	out := &s{}
	if _, err := UnpackInovance(bytes.NewReader(buf.Bytes()), out, AlignDefault); err != nil {
		t.Fatalf("UnpackInovance: %v", err)
	}
	if *out != v {
		t.Fatalf("round trip mismatch: have %v want %v", out.M, v.M)
	}
}

// Nested structs: the nested layout is aligned to its largest member and its
// size is rounded up to that, matching §4.4.1.
func TestInovanceNestedStructLayout(t *testing.T) {
	type sub struct {
		A int32
		B bool
	}
	type outer struct {
		M0  bool
		Sub sub
		M2  int16
	}

	// default: M0@0..1, Sub@4..11 (align 4, inner size 8), M2@12..13, total 16
	size, err := InovanceStructSize(outer{}, AlignDefault)
	if err != nil {
		t.Fatalf("InovanceStructSize: %v", err)
	}
	if size != 16 {
		t.Fatalf("default alignment nested size = %d, want 16", size)
	}

	// inoproshop(1): M0@0, Sub@1..5 (A@1..4, B@5), M2@6..7, total 8
	size, err = InovanceStructSize(outer{}, AlignInoProShop)
	if err != nil {
		t.Fatalf("InovanceStructSize: %v", err)
	}
	if size != 8 {
		t.Fatalf("inoproshop alignment nested size = %d, want 8", size)
	}

	in := outer{M0: true, Sub: sub{A: 0x01020304, B: true}, M2: -2}
	buf := &bytes.Buffer{}
	if _, err := PackInovance(buf, in, AlignDefault); err != nil {
		t.Fatalf("PackInovance: %v", err)
	}
	want := []byte{
		0x01, 0x00, 0x00, 0x00, // M0 + padding
		0x04, 0x03, 0x02, 0x01, // Sub.A
		0x01, 0x00, 0x00, 0x00, // Sub.B (2 bytes + struct rounding)
		0xFE, 0xFF, 0x00, 0x00, // M2 + struct rounding
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("nested pack mismatch\n have % X\n want % X", buf.Bytes(), want)
	}

	out := &outer{}
	if _, err := UnpackInovance(bytes.NewReader(buf.Bytes()), out, AlignDefault); err != nil {
		t.Fatalf("UnpackInovance: %v", err)
	}
	if *out != in {
		t.Fatalf("round trip mismatch: have %+v want %+v", *out, in)
	}
}

func TestInovanceLayoutRejectsUnsupportedMembers(t *testing.T) {
	type withPtr struct {
		P *int32
	}
	type withSlice struct {
		S []int16
	}
	type withString struct {
		S string
	}

	if _, err := InovanceStructSize(withPtr{}, AlignDefault); err == nil {
		t.Error("pointer member: expected an error")
	} else if !strings.Contains(err.Error(), "pointer") {
		t.Errorf("pointer member: unexpected error %v", err)
	}
	if _, err := InovanceStructSize(withSlice{}, AlignDefault); err == nil {
		t.Error("slice member: expected an error")
	} else if !strings.Contains(err.Error(), "fixed size array") {
		t.Errorf("slice member: unexpected error %v", err)
	}
	if _, err := InovanceStructSize(withString{}, AlignDefault); err == nil {
		t.Error("string member: expected an error")
	} else if !strings.Contains(err.Error(), "STRING<N>") {
		t.Errorf("string member: unexpected error %v", err)
	}
}

func TestInovanceTypeMapping(t *testing.T) {
	tests := []struct {
		in    any
		want  CIPType
		elems int
	}{
		{in: true, want: invTypeBOOL, elems: 1},
		{in: []bool{true, false}, want: invTypeBOOL, elems: 2},
		{in: int8(1), want: invTypeSINT, elems: 1},
		{in: byte(1), want: invTypeBYTE, elems: 1},
		{in: int16(1), want: invTypeINT, elems: 1},
		{in: uint16(1), want: invTypeUINT, elems: 1},
		{in: int32(1), want: invTypeDINT, elems: 1},
		{in: uint32(1), want: invTypeUDINT, elems: 1},
		{in: int64(1), want: invTypeLINT, elems: 1},
		{in: uint64(1), want: invTypeULINT, elems: 1},
		{in: float32(1), want: invTypeREAL, elems: 1},
		{in: float64(1), want: invTypeLREAL, elems: 1},
		{in: "abc", want: invTypeSTRING, elems: 1},
		{in: invUDT1{}, want: invTypeSTRUCT, elems: 1},
		{in: []int32{1, 2, 3}, want: invTypeDINT, elems: 3},
	}

	for _, tt := range tests {
		got, elems, err := invGoVarToCIPType(tt.in)
		if err != nil {
			t.Errorf("invGoVarToCIPType(%T): %v", tt.in, err)
			continue
		}
		if got != tt.want || elems != tt.elems {
			t.Errorf("invGoVarToCIPType(%T) = %s/%d, want %s/%d",
				tt.in, invDescribeType(got), elems, invDescribeType(tt.want), tt.elems)
		}
	}
}

func TestInovanceNormalizeType(t *testing.T) {
	// 0xA2 and 0xD0 are the codes the Logix path would not understand.
	if got, err := invNormalizeType(CIPType(0xA2)); err != nil || got != invTypeSTRUCT {
		t.Errorf("normalize(0xA2) = %v, %v", got, err)
	}
	if got, err := invNormalizeType(CIPType(0xD0)); err != nil || got != invTypeSTRING {
		t.Errorf("normalize(0xD0) = %v, %v", got, err)
	}
	if got, err := invNormalizeType(CIPType(0xD5)); err != nil || got != invTypeWSTRING {
		t.Errorf("normalize(0xD5) = %v, %v", got, err)
	}
	if _, err := invNormalizeType(CIPType(0x42)); err == nil {
		t.Error("normalize(0x42): expected an error")
	}
}

func TestInovanceSliceOfStructs(t *testing.T) {
	in := []invUDT1{{M0: 1, M1: 2}, {M0: 3, M1: 4}}
	size, err := InovanceStructSize(in, AlignDefault)
	if err != nil {
		t.Fatalf("InovanceStructSize: %v", err)
	}
	if size != 16 {
		t.Fatalf("slice size = %d, want 16", size)
	}

	buf := &bytes.Buffer{}
	n, err := PackInovance(buf, in, AlignDefault)
	if err != nil {
		t.Fatalf("PackInovance: %v", err)
	}
	if n != 16 {
		t.Fatalf("packed %d bytes, want 16", n)
	}

	out := make([]invUDT1, 2)
	if _, err := UnpackInovance(bytes.NewReader(buf.Bytes()), out, AlignDefault); err != nil {
		t.Fatalf("UnpackInovance: %v", err)
	}
	if !reflect.DeepEqual(out, in) {
		t.Fatalf("round trip mismatch: have %+v want %+v", out, in)
	}
}

func TestDialectDefaultsToLogix(t *testing.T) {
	c := NewClient("127.0.0.1")
	if c.Dialect != DialectLogix {
		t.Errorf("new client dialect = %v, want logix", c.Dialect)
	}
	if c.isInovance() {
		t.Error("new client should not be inovance")
	}
	if c.inovanceAlign() != AlignDefault {
		t.Errorf("default alignment = %v, want default", c.inovanceAlign())
	}

	if err := c.UseInovance(AlignInoProShop); err != nil {
		t.Fatalf("UseInovance: %v", err)
	}
	if !c.isInovance() || c.inovanceAlign() != AlignInoProShop {
		t.Errorf("UseInovance did not take effect: dialect=%v align=%v", c.Dialect, c.inovanceAlign())
	}
	if err := c.UseInovance(invAlignUnsupported); err == nil {
		t.Error("UseInovance with an unsupported alignment should fail")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
