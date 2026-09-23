package gologix

import (
	"fmt"
	"reflect"
	"strings"
)

// invIsMemberTag reports whether a tag path addresses a struct member.
//
// The Inovance tag syntax separates struct members with "." and array elements
// with "[n]" (documentation §2.8), so a top level tag never contains a dot.
// The member/top level distinction matters because the BOOL rules differ
// between §4.2 (top level) and §4.4.2 (struct members).
func invIsMemberTag(tag string) bool {
	return strings.Contains(tag, ".")
}

// invSerializeValue writes the Inovance payload for a single tag write into the
// request item. It is only reached when the client dialect is Inovance; the
// Logix path keeps using CIPItem.Serialize.
func (client *Client) invSerializeValue(item *CIPItem, tag string, value any, datatype CIPType) error {
	rv := reflect.ValueOf(value)
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return fmt.Errorf("value for %s is a nil pointer", tag)
		}
		rv = rv.Elem()
	}

	switch datatype {
	case invTypeBOOL:
		return client.invSerializeBool(item, tag, rv)

	case invTypeSTRING, invTypeWSTRING:
		return fmt.Errorf("writing %s to %s is not supported yet (STRING/WSTRING support is planned for P1)",
			invDescribeType(datatype), tag)

	case invTypeSTRUCT:
		return fmt.Errorf("writing a struct to %s must go through the UDT write path", tag)

	default:
		// Atomic scalars and arrays use the same little endian element layout
		// as the Logix path (documentation §2.21/§2.22).
		return item.Serialize(value)
	}
}

// invSerializeBool writes the payload of a BOOL / BOOL[n] write.
//
// The rules that apply here (documentation V2.0.2.8):
//
//	§4.2      top level BOOL     1 byte, BOOL[n] n bytes (both alignments)
//	§4.4.2(1) BOOL member        2 bytes under the default alignment, LSB valid
//	§4.4.2(2) BOOL[n] member     bit packed, rounded up to 16 bits
//	§4.4.2(2) InoProShop align=1 1 byte per bool for members and arrays alike
func (client *Client) invSerializeBool(item *CIPItem, tag string, rv reflect.Value) error {
	mode := client.inovanceAlign()
	member := invIsMemberTag(tag)

	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		if rv.Kind() != reflect.Bool {
			return fmt.Errorf("tag %s: expected a bool value, got %v", tag, rv.Kind())
		}
		b := byte(0)
		if rv.Bool() {
			b = 1
		}
		if mode == AlignDefault && member {
			// the second byte is the padding half of the 2-byte member
			_, err := item.Write([]byte{b, 0x00})
			return err
		}
		_, err := item.Write([]byte{b})
		return err
	}

	payload, err := invBoolArrayPayload(rv, mode, member)
	if err != nil {
		return fmt.Errorf("tag %s: %w", tag, err)
	}
	_, err = item.Write(payload)
	return err
}

// invBoolArrayPayload renders a BOOL[n] value as it appears on the wire.
//
// rv must be a slice or array of bool.
func invBoolArrayPayload(rv reflect.Value, mode InovanceAlign, member bool) ([]byte, error) {
	n := rv.Len()
	if n == 0 {
		return nil, nil
	}
	if rv.Index(0).Kind() != reflect.Bool {
		return nil, fmt.Errorf("expected a bool array, got %v", rv.Type())
	}

	if mode == AlignInoProShop || !member {
		// one byte per element: Bool[17] occupies 17 bytes
		out := make([]byte, n)
		for i := 0; i < n; i++ {
			if rv.Index(i).Bool() {
				out[i] = 1
			}
		}
		return out, nil
	}

	// default alignment, struct member: bits packed low -> high, the total
	// rounded up to a multiple of 16 bits (Bool[10] = 2 bytes, Bool[17] = 4)
	out := make([]byte, invRoundUp(n, 16)/8)
	for i := 0; i < n; i++ {
		if rv.Index(i).Bool() {
			out[i/8] |= 1 << uint(i%8)
		}
	}
	return out, nil
}
