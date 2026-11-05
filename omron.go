package gologix

// Omron NJ/NX specifics. Every rule here is backed by the W506 manual and/or
// the OMRON Compolet assemblies; docs/omron-nx-nj-eip.md records the evidence.
const (
	// omronConnectionSize is the Large_Forward_Open connection size limit for
	// NJ/NX controllers (W506 §7-1-2): 1994 bytes for every model except the
	// NX701, which allows 8192.
	omronConnectionSize = 1994

	// omronBoolElementSize is the number of bytes a single BOOL occupies on the
	// wire: the status byte followed by the forced set/reset information, which
	// must be zero when writing.
	omronBoolElementSize = 2

	// omronTypeBitArray is the vendor specific "bit array" data type the OMRON
	// client substitutes for BOOL when a BOOL array is accessed as a whole
	// (Compolet CipOmronDataConverter.BoolsToBitArray). The array is then
	// transmitted bit packed, 16 bits per WORD.
	omronTypeBitArray CIPType = 0xFF
)

// omronSerializeValue appends value to a write request using the OMRON wire
// rules: a BOOL is two bytes, the status (0 or 1) followed by the forced
// set/reset byte, which must be zero when writing.
func omronSerializeValue(item *CIPItem, value any) error {
	if b, ok := value.(bool); ok {
		var buf [omronBoolElementSize]byte
		if b {
			buf[0] = 1
		}
		return item.Serialize(buf[:])
	}
	return item.Serialize(value)
}
