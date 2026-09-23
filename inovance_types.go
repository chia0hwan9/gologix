package gologix

import (
	"fmt"
	"reflect"
)

// Inovance wire type codes. Reference: "EIP 标签通信库使用说明" V2.0.2.8,
// §2.2 数据类型标识码 and §4.1 基础数据类型.
//
// The atomic codes are identical to the CIP codes this library already uses;
// the differences from Logix are:
//
//	STRING  0xD0  (Logix: 0xA0 struct with an internal 0xFF marker)
//	WSTRING 0xD5  (not supported by Logix path at all)
//	STRUCT  0xA2  (Logix: 0xA0)
//	uint64  ULINT 0xC9 (the Logix path uses LWORD 0xD4)
const (
	invTypeBOOL    = CIPTypeBOOL            // 0xC1
	invTypeSINT    = CIPTypeSINT            // 0xC2
	invTypeINT     = CIPTypeINT             // 0xC3
	invTypeDINT    = CIPTypeDINT            // 0xC4
	invTypeLINT    = CIPTypeLINT            // 0xC5
	invTypeUSINT   = CIPTypeUSINT           // 0xC6
	invTypeUINT    = CIPTypeUINT            // 0xC7
	invTypeUDINT   = CIPTypeUDINT           // 0xC8
	invTypeULINT   = CIPTypeULINT           // 0xC9
	invTypeREAL    = CIPTypeREAL            // 0xCA
	invTypeLREAL   = CIPTypeLREAL           // 0xCB
	invTypeSTRING  = CIPTypeSTRING_UNKNOWN  // 0xD0
	invTypeBYTE    = CIPTypeBYTE            // 0xD1
	invTypeWORD    = CIPTypeWORD            // 0xD2
	invTypeDWORD   = CIPTypeDWORD           // 0xD3
	invTypeLWORD   = CIPTypeLWORD           // 0xD4
	invTypeWSTRING = CIPTypeSTRING_UNKNOWN2 // 0xD5
	invTypeSTRUCT  = CIPType(0xA2)
)

// invKind classifies a wire type into the shapes that need different handling.
type invKind int

const (
	invKindUnknown invKind = iota
	invKindAtomic
	invKindStruct
	invKindString
	invKindWString
)

func (k invKind) String() string {
	switch k {
	case invKindAtomic:
		return "atomic"
	case invKindStruct:
		return "struct"
	case invKindString:
		return "string"
	case invKindWString:
		return "wstring"
	default:
		return "unknown"
	}
}

// invKindOf returns the shape of a wire type.
func invKindOf(t CIPType) invKind {
	switch t {
	case invTypeSTRUCT:
		return invKindStruct
	case invTypeSTRING:
		return invKindString
	case invTypeWSTRING:
		return invKindWString
	case invTypeBOOL, invTypeSINT, invTypeINT, invTypeDINT, invTypeLINT,
		invTypeUSINT, invTypeUINT, invTypeUDINT, invTypeULINT,
		invTypeREAL, invTypeLREAL,
		invTypeBYTE, invTypeWORD, invTypeDWORD, invTypeLWORD:
		return invKindAtomic
	default:
		return invKindUnknown
	}
}

// invElementSize returns the size on the wire of a single element of t, for
// elements that have a fixed size.
//
// A top level BOOL is 1 byte in both alignment modes (§4.2), which is why the
// 2-byte BOOL rule does not appear here - it only applies to struct members.
// STRING/WSTRING/STRUCT are variable or context dependent and return 0.
func invElementSize(t CIPType) int {
	switch t {
	case invTypeBOOL, invTypeSINT, invTypeUSINT, invTypeBYTE:
		return 1
	case invTypeINT, invTypeUINT, invTypeWORD:
		return 2
	case invTypeDINT, invTypeUDINT, invTypeREAL, invTypeDWORD:
		return 4
	case invTypeLINT, invTypeULINT, invTypeLREAL, invTypeLWORD:
		return 8
	default:
		return 0
	}
}

// invTypeFromGoKind maps a Go kind to the Inovance wire type used outside of
// struct layouts (top level tags and array elements).
func invTypeFromGoKind(k reflect.Kind) (CIPType, error) {
	switch k {
	case reflect.Bool:
		return invTypeBOOL, nil
	case reflect.Int8:
		return invTypeSINT, nil
	case reflect.Uint8:
		return invTypeBYTE, nil
	case reflect.Int16:
		return invTypeINT, nil
	case reflect.Uint16:
		return invTypeUINT, nil
	case reflect.Int32:
		return invTypeDINT, nil
	case reflect.Uint32:
		return invTypeUDINT, nil
	case reflect.Int64:
		return invTypeLINT, nil
	case reflect.Uint64:
		return invTypeULINT, nil
	case reflect.Float32:
		return invTypeREAL, nil
	case reflect.Float64:
		return invTypeLREAL, nil
	case reflect.String:
		return invTypeSTRING, nil
	default:
		return CIPTypeUnknown, fmt.Errorf("no Inovance wire type for Go kind %v", k)
	}
}

// invGoVarToCIPType maps a Go value to its Inovance wire type and element
// count. It is the Inovance counterpart of GoVarToCIPType and differs in that
// it knows about []bool, uses 0xD0 for strings and 0xA2 for structs.
func invGoVarToCIPType(v any) (CIPType, int, error) {
	if v == nil {
		return CIPTypeUnknown, 0, fmt.Errorf("cannot determine the Inovance wire type of a nil value")
	}
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return CIPTypeUnknown, 0, fmt.Errorf("cannot determine the Inovance wire type of a nil pointer")
		}
		rv = rv.Elem()
	}

	switch rv.Kind() {
	case reflect.Struct:
		return invTypeSTRUCT, 1, nil
	case reflect.Array, reflect.Slice:
		n := rv.Len()
		if n == 0 {
			return CIPTypeUnknown, 0, fmt.Errorf("cannot determine the Inovance wire type of an empty slice")
		}
		et, err := invElemTypeOf(rv.Type().Elem())
		if err != nil {
			return CIPTypeUnknown, 0, err
		}
		return et, n, nil
	default:
		t, err := invTypeFromGoKind(rv.Kind())
		if err != nil {
			return CIPTypeUnknown, 0, err
		}
		return t, 1, nil
	}
}

// invElemTypeOf maps an element type to its wire type (arrays/slices).
func invElemTypeOf(t reflect.Type) (CIPType, error) {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() == reflect.Struct {
		return invTypeSTRUCT, nil
	}
	return invTypeFromGoKind(t.Kind())
}

// invNormalizeType maps a wire type code seen in a response to the Inovance
// type this library works with, so the Inovance path never depends on the
// Logix-only meanings of 0xA0/0xFF.
//
// Not wired yet: nothing outside the tests calls this today. It is the
// response dispatch table for the P2 item "multi tag / response type
// dispatch" (reading a whole struct answers with 0xA2, a string with 0xD0).
func invNormalizeType(t CIPType) (CIPType, error) {
	if _, ok := invResponseTypes[t]; ok {
		return t, nil
	}
	switch t {
	case CIPTypeStruct:
		// Some controllers answer a struct read with the generic 0xA0.
		return invTypeSTRUCT, nil
	case CIPTypeSTRING:
		return invTypeSTRING, nil
	}
	return CIPTypeUnknown, fmt.Errorf("unknown Inovance response type 0x%02X", byte(t))
}

// invResponseTypes is the set of type codes the Inovance path can decode.
var invResponseTypes = map[CIPType]struct{}{
	invTypeBOOL: {}, invTypeSINT: {}, invTypeINT: {}, invTypeDINT: {},
	invTypeLINT: {}, invTypeUSINT: {}, invTypeUINT: {}, invTypeUDINT: {},
	invTypeULINT: {}, invTypeREAL: {}, invTypeLREAL: {}, invTypeSTRING: {},
	invTypeBYTE: {}, invTypeWORD: {}, invTypeDWORD: {}, invTypeLWORD: {},
	invTypeWSTRING: {}, invTypeSTRUCT: {},
}

// invDescribeType returns a human readable name for error messages.
func invDescribeType(t CIPType) string {
	switch t {
	case invTypeSTRUCT:
		return "STRUCT(0xA2)"
	case invTypeSTRING:
		return "STRING(0xD0)"
	case invTypeWSTRING:
		return "WSTRING(0xD5)"
	default:
		return fmt.Sprintf("%v(0x%02X)", t, byte(t))
	}
}
