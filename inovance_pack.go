package gologix

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"reflect"
)

// ---------------------------------------------------------------------------
// Inovance struct layout ("对齐规则")
//
// Reference: 汇川《EIP 标签通信库使用说明》V2.0.2.8, §4.4.1 / §4.4.2 / §4.4.3.
// The byte level fixtures used by the tests come from the layout tables on
// pages 16/17 of that document. A local copy lives at
// docs/汇川EIP标签通信库使用说明V2.0.2.8.pdf (indexed in docs/resources.md).
// For a side by side comparison of every BOOL case (top level vs struct member,
// Logix vs both Inovance alignments) see docs/bool-handling.md.
//
// Status: the engine below is implemented and unit tested, but it is NOT wired
// into any read/write path yet, and it is not needed for struct *member*
// access - a member like "Stru.mem" is addressed by name and the PLC works out
// the offset itself. Whole struct reads/writes are still refused on purpose:
// write_udt returns an explicit error for the Inovance dialect, and a struct
// read comes back with type 0xA2 which readValue does not know, so it fails
// loudly instead of returning shifted bytes. Wiring this up is the P2 item
// "struct whole-tag access".
//
// AlignDefault (AT_DEFAULT, 0x00) - what the Easy series / AutoShop uses,
// where no struct alignment parameter can be configured:
//
//	BOOL              align 2, size 2   (only bit 0 of the first byte is valid)
//	BOOL[n]           align 2, size roundUp(n,16)/8, bits packed low -> high
//	SINT/USINT/BYTE   align 1, size 1
//	INT/UINT/WORD     align 2, size 2
//	DINT/UDINT/REAL/DWORD    align 4, size 4
//	LINT/ULINT/LREAL/LWORD   align 8, size 8
//	arrays            elements packed tight, aligned to the element type
//	nested struct     align = largest member size, size rounded up to it
//	STRING<N>         align 1, size N (transmitted at max length, last byte 0)
//
// AlignInoProShop (AT_INOPROSHOP, 0x01) with the project alignment parameter
// set to 1: everything is 1-byte aligned and nothing is padded, so a BOOL
// member takes 1 byte and BOOL[n] takes n bytes.
//
// A top level BOOL is 1 byte in both modes (§4.2); the 2-byte rule applies to
// struct members only.
// ---------------------------------------------------------------------------

// invPad returns the number of padding bytes needed to reach alignment a.
func invPad(pos, a int) int {
	if a <= 1 {
		return 0
	}
	if rem := pos % a; rem != 0 {
		return a - rem
	}
	return 0
}

// invRoundUp rounds n up to the next multiple of a.
func invRoundUp(n, a int) int {
	if a <= 1 {
		return n
	}
	if rem := n % a; rem != 0 {
		return n + a - rem
	}
	return n
}

// invMemberKind is the shape of one struct member.
type invMemberKind int

const (
	invMemberAtomic    invMemberKind = iota
	invMemberBool                    // a single BOOL member (2 bytes under AlignDefault)
	invMemberBoolArray               // BOOL[n] member (bit packed under AlignDefault)
	invMemberStruct                  // nested struct, or an array of them
	invMemberString                  // STRING<N> - implemented in P1
)

// invMember is the resolved layout of one struct member.
type invMember struct {
	name   string
	index  int
	kind   invMemberKind
	ctype  CIPType // wire type, for arrays the element type
	elems  int
	array  bool // true when the Go field is an array/slice
	offset int  // byte offset inside the struct
	align  int
	size   int
	nested *invStructLayout // for invMemberStruct
}

// invStructLayout is the resolved layout of a struct type.
type invStructLayout struct {
	fields []invMember
	align  int
	size   int
}

// invLayoutOfStructType works out the byte layout of a struct type under the
// given alignment mode.
func invLayoutOfStructType(t reflect.Type, mode InovanceAlign) (*invStructLayout, error) {
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("Inovance struct layout expects a struct, got %v", t.Kind())
	}
	layout := &invStructLayout{align: 1}
	pos := 0
	maxMember := 1

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" { // unexported
			continue
		}
		if tag, ok := f.Tag.Lookup("inv"); ok && tag == "-" {
			continue
		}
		m, err := invLayoutOfField(f, mode)
		if err != nil {
			return nil, err
		}
		pos += invPad(pos, m.align)
		m.offset = pos
		pos += m.size
		if m.align > maxMember {
			maxMember = m.align
		}
		layout.fields = append(layout.fields, m)
	}

	if mode == AlignDefault {
		layout.align = maxMember
	} else {
		layout.align = 1
	}
	layout.size = invRoundUp(pos, layout.align)
	return layout, nil
}

// invLayoutOfField works out the layout of a single struct member.
func invLayoutOfField(f reflect.StructField, mode InovanceAlign) (invMember, error) {
	m := invMember{name: f.Name, index: f.Index[0]}

	t := f.Type
	if t.Kind() == reflect.Ptr {
		return m, fmt.Errorf("Inovance struct member %s: pointer members are not supported (the PLC stores values inline)", f.Name)
	}

	elems := 1
	isArray := false
	switch t.Kind() {
	case reflect.Array:
		elems = t.Len()
		isArray = true
		t = t.Elem()
	case reflect.Slice:
		return m, fmt.Errorf("Inovance struct member %s: use a fixed size array, a slice has no length in the PLC layout", f.Name)
	}
	m.elems = elems
	m.array = isArray

	if elems == 0 {
		m.size = 0
		m.align = 1
		return m, nil
	}

	switch {
	case t.Kind() == reflect.Bool:
		if !isArray {
			m.kind = invMemberBool
			if mode == AlignDefault {
				m.align, m.size = 2, 2
			} else {
				m.align, m.size = 1, 1
			}
		} else {
			m.kind = invMemberBoolArray
			if mode == AlignDefault {
				// bits packed low -> high, total rounded up to 16 bits
				m.align, m.size = 2, invRoundUp(elems, 16)/8
			} else {
				m.align, m.size = 1, elems
			}
		}

	case t.Kind() == reflect.Struct:
		nested, err := invLayoutOfStructType(t, mode)
		if err != nil {
			return m, fmt.Errorf("Inovance struct member %s: %w", f.Name, err)
		}
		m.kind = invMemberStruct
		m.nested = nested
		m.align = nested.align
		m.size = nested.size * elems

	case t.Kind() == reflect.String:
		return m, fmt.Errorf("Inovance struct member %s: STRING members are not supported; declare the member as a byte array (ARRAY[0..N-1] OF BYTE, WSTRING as ARRAY OF WORD) and use a [N]byte/[N]uint16 field instead", f.Name)

	default:
		ct, err := invTypeFromGoKind(t.Kind())
		if err != nil {
			return m, fmt.Errorf("Inovance struct member %s: %w", f.Name, err)
		}
		m.kind = invMemberAtomic
		m.ctype = ct
		es := invElementSize(ct)
		if mode == AlignDefault {
			m.align = es
		} else {
			// InoProShop alignment parameter = 1: members are packed with no
			// padding at all, so a DINT is not 4-byte aligned either.
			m.align = 1
		}
		m.size = es * elems
	}

	return m, nil
}

// invElemValue returns the i-th element value of a field, handling scalar
// fields (which are not indexable) transparently.
func invElemValue(fv reflect.Value, m invMember, i int) reflect.Value {
	if m.array {
		return fv.Index(i)
	}
	return fv
}

// InovanceStructSize returns the number of bytes a struct (or a slice/array of
// structs) occupies on the wire under the given alignment mode. It is used to
// validate write payloads before they are sent.
func InovanceStructSize(data any, mode InovanceAlign) (int, error) {
	if data == nil {
		return 0, fmt.Errorf("InovanceStructSize: nil value")
	}
	t := reflect.TypeOf(data)
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		layout, err := invLayoutOfStructType(t, mode)
		if err != nil {
			return 0, err
		}
		return layout.size, nil
	case reflect.Array, reflect.Slice:
		et := t.Elem()
		for et.Kind() == reflect.Ptr {
			et = et.Elem()
		}
		layout, err := invLayoutOfStructType(et, mode)
		if err != nil {
			return 0, err
		}
		n := 1
		if rv := reflect.ValueOf(data); rv.Kind() != reflect.Ptr || !rv.IsNil() {
			rv = reflect.Indirect(reflect.ValueOf(data))
			if rv.Kind() == reflect.Array || rv.Kind() == reflect.Slice {
				n = rv.Len()
			}
		}
		return layout.size * n, nil
	default:
		return 0, fmt.Errorf("InovanceStructSize expects a struct or an array of structs, got %v", t.Kind())
	}
}

// PackInovance serializes a struct (or a slice/array of structs) using the
// Inovance layout rules. It is the Inovance counterpart of Pack and never
// touches the Logix code path.
func PackInovance(w io.Writer, data any, mode InovanceAlign) (int, error) {
	rv := reflect.ValueOf(data)
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return 0, fmt.Errorf("PackInovance: nil pointer")
		}
		rv = rv.Elem()
	}

	switch rv.Kind() {
	case reflect.Struct:
		buf, err := invPackStruct(rv, mode)
		if err != nil {
			return 0, err
		}
		return w.Write(buf)

	case reflect.Slice, reflect.Array:
		total := 0
		for i := 0; i < rv.Len(); i++ {
			ev := rv.Index(i)
			for ev.Kind() == reflect.Ptr {
				ev = ev.Elem()
			}
			buf, err := invPackStruct(ev, mode)
			if err != nil {
				return total, fmt.Errorf("element %d: %w", i, err)
			}
			n, err := w.Write(buf)
			total += n
			if err != nil {
				return total, err
			}
		}
		return total, nil

	default:
		return 0, fmt.Errorf("PackInovance expects a struct or an array of structs, got %v", rv.Kind())
	}
}

// invPackStruct renders a single struct into its wire bytes.
func invPackStruct(rv reflect.Value, mode InovanceAlign) ([]byte, error) {
	layout, err := invLayoutOfStructType(rv.Type(), mode)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, layout.size)
	if err := invPackFields(buf, 0, rv, layout, mode); err != nil {
		return nil, err
	}
	return buf, nil
}

func invPackFields(buf []byte, base int, rv reflect.Value, layout *invStructLayout, mode InovanceAlign) error {
	for _, m := range layout.fields {
		off := base + m.offset
		fv := rv.Field(m.index)

		switch m.kind {
		case invMemberBool:
			if fv.Bool() {
				buf[off] = 1
			}

		case invMemberBoolArray:
			for i := 0; i < m.elems; i++ {
				if !fv.Index(i).Bool() {
					continue
				}
				if mode == AlignInoProShop {
					buf[off+i] = 1
				} else {
					buf[off+i/8] |= 1 << uint(i%8)
				}
			}

		case invMemberStruct:
			for i := 0; i < m.elems; i++ {
				ev := invElemValue(fv, m, i)
				for ev.Kind() == reflect.Ptr {
					ev = ev.Elem()
				}
				if err := invPackFields(buf, off+i*m.nested.size, ev, m.nested, mode); err != nil {
					return fmt.Errorf("member %s: %w", m.name, err)
				}
			}

		case invMemberAtomic:
			size := invElementSize(m.ctype)
			for i := 0; i < m.elems; i++ {
				if err := invPutAtomic(buf, off+i*size, m.ctype, invElemValue(fv, m, i)); err != nil {
					return fmt.Errorf("member %s: %w", m.name, err)
				}
			}

		default:
			return fmt.Errorf("member %s: unsupported member kind %v", m.name, m.kind)
		}
	}
	return nil
}

// invPutAtomic writes one atomic value little endian.
func invPutAtomic(buf []byte, off int, t CIPType, v reflect.Value) error {
	switch t {
	case invTypeBOOL:
		if v.Bool() {
			buf[off] = 1
		}
	case invTypeSINT:
		buf[off] = byte(int8(v.Int()))
	case invTypeUSINT, invTypeBYTE:
		buf[off] = byte(v.Uint())
	case invTypeINT:
		binary.LittleEndian.PutUint16(buf[off:], uint16(int16(v.Int())))
	case invTypeUINT, invTypeWORD:
		binary.LittleEndian.PutUint16(buf[off:], uint16(v.Uint()))
	case invTypeDINT:
		binary.LittleEndian.PutUint32(buf[off:], uint32(int32(v.Int())))
	case invTypeUDINT, invTypeDWORD:
		binary.LittleEndian.PutUint32(buf[off:], uint32(v.Uint()))
	case invTypeLINT:
		binary.LittleEndian.PutUint64(buf[off:], uint64(v.Int()))
	case invTypeULINT, invTypeLWORD:
		binary.LittleEndian.PutUint64(buf[off:], v.Uint())
	case invTypeREAL:
		binary.LittleEndian.PutUint32(buf[off:], math.Float32bits(float32(v.Float())))
	case invTypeLREAL:
		binary.LittleEndian.PutUint64(buf[off:], math.Float64bits(v.Float()))
	default:
		return fmt.Errorf("unsupported atomic Inovance type %s", invDescribeType(t))
	}
	return nil
}

// invGetAtomic reads one atomic value little endian.
func invGetAtomic(buf []byte, off int, t CIPType, v reflect.Value) error {
	switch t {
	case invTypeBOOL:
		v.SetBool(buf[off]&1 != 0)
	case invTypeSINT:
		v.SetInt(int64(int8(buf[off])))
	case invTypeUSINT, invTypeBYTE:
		v.SetUint(uint64(buf[off]))
	case invTypeINT:
		v.SetInt(int64(int16(binary.LittleEndian.Uint16(buf[off:]))))
	case invTypeUINT, invTypeWORD:
		v.SetUint(uint64(binary.LittleEndian.Uint16(buf[off:])))
	case invTypeDINT:
		v.SetInt(int64(int32(binary.LittleEndian.Uint32(buf[off:]))))
	case invTypeUDINT, invTypeDWORD:
		v.SetUint(uint64(binary.LittleEndian.Uint32(buf[off:])))
	case invTypeLINT:
		v.SetInt(int64(binary.LittleEndian.Uint64(buf[off:])))
	case invTypeULINT, invTypeLWORD:
		v.SetUint(binary.LittleEndian.Uint64(buf[off:]))
	case invTypeREAL:
		v.SetFloat(float64(math.Float32frombits(binary.LittleEndian.Uint32(buf[off:]))))
	case invTypeLREAL:
		v.SetFloat(math.Float64frombits(binary.LittleEndian.Uint64(buf[off:])))
	default:
		return fmt.Errorf("unsupported atomic Inovance type %s", invDescribeType(t))
	}
	return nil
}

// UnpackInovance decodes wire bytes into a struct (or a slice/array of
// structs) using the Inovance layout rules. It is the Inovance counterpart of
// Unpack.
func UnpackInovance(r io.Reader, data any, mode InovanceAlign) (int, error) {
	rv := reflect.ValueOf(data)
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return 0, fmt.Errorf("UnpackInovance: nil pointer")
		}
		rv = rv.Elem()
	}

	switch rv.Kind() {
	case reflect.Struct:
		layout, err := invLayoutOfStructType(rv.Type(), mode)
		if err != nil {
			return 0, err
		}
		buf := make([]byte, layout.size)
		n, err := io.ReadFull(r, buf)
		if err != nil {
			return n, fmt.Errorf("reading %d bytes of %v: %w", layout.size, rv.Type(), err)
		}
		if err := invUnpackFields(buf, 0, rv, layout, mode); err != nil {
			return n, err
		}
		return n, nil

	case reflect.Slice, reflect.Array:
		total := 0
		for i := 0; i < rv.Len(); i++ {
			n, err := UnpackInovance(r, rv.Index(i).Addr().Interface(), mode)
			total += n
			if err != nil {
				return total, fmt.Errorf("element %d: %w", i, err)
			}
		}
		return total, nil

	default:
		return 0, fmt.Errorf("UnpackInovance expects a pointer to a struct or to an array of structs, got %v", rv.Kind())
	}
}

func invUnpackFields(buf []byte, base int, rv reflect.Value, layout *invStructLayout, mode InovanceAlign) error {
	for _, m := range layout.fields {
		off := base + m.offset
		fv := rv.Field(m.index)

		switch m.kind {
		case invMemberBool:
			fv.SetBool(buf[off]&1 != 0)

		case invMemberBoolArray:
			for i := 0; i < m.elems; i++ {
				var on bool
				if mode == AlignInoProShop {
					on = buf[off+i]&1 != 0
				} else {
					on = buf[off+i/8]&(1<<uint(i%8)) != 0
				}
				fv.Index(i).SetBool(on)
			}

		case invMemberStruct:
			for i := 0; i < m.elems; i++ {
				ev := invElemValue(fv, m, i)
				if err := invUnpackFields(buf, off+i*m.nested.size, ev, m.nested, mode); err != nil {
					return fmt.Errorf("member %s: %w", m.name, err)
				}
			}

		case invMemberAtomic:
			size := invElementSize(m.ctype)
			for i := 0; i < m.elems; i++ {
				if err := invGetAtomic(buf, off+i*size, m.ctype, invElemValue(fv, m, i)); err != nil {
					return fmt.Errorf("member %s: %w", m.name, err)
				}
			}

		default:
			return fmt.Errorf("member %s: unsupported member kind %v", m.name, m.kind)
		}
	}
	return nil
}
