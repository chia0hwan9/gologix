package gologix

import (
	"context"
	"encoding/binary"
	"fmt"
)

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

	// omronBitsPerWord is the packing granularity of the bit array type:
	// SysmacDataConverter.WordDataToBitArray turns one 16 bit WORD into a
	// bool[16], and W506 §7-7-4 shows a BOOL[2][3] fitting in a single WORD.
	omronBitsPerWord = 16
)

// omronSerializeValue appends value to a write request using the OMRON wire
// rules: a BOOL is two bytes, the status (0 or 1) followed by the forced
// set/reset byte, which must be zero when writing. A whole BOOL array is sent
// as a bit array packed 16 bits per WORD (Compolet BoolsToBitArray /
// BitArrayToWordData).
func omronSerializeValue(item *CIPItem, value any) error {
	switch v := value.(type) {
	case bool:
		var buf [omronBoolElementSize]byte
		if v {
			buf[0] = 1
		}
		return item.Serialize(buf[:])
	case []bool:
		return item.Serialize(omronPackBoolWords(v))
	default:
		return item.Serialize(value)
	}
}

// omronPackBoolWords packs bools low bit first into 16 bit WORDs, the layout
// the bit array type (0xFF) uses on the wire.
func omronPackBoolWords(bools []bool) []byte {
	words := (len(bools) + omronBitsPerWord - 1) / omronBitsPerWord
	out := make([]byte, words*2)
	for i, b := range bools {
		if b {
			out[i/8] |= 1 << uint(i%8)
		}
	}
	return out
}

// omronReadBoolSlice reads a whole BOOL array.
//
// The controller may answer in three shapes, and the request cannot force one
// of them from this side, so the reply length decides how the data is decoded
// (all three are documented or observed, see docs/omron-nx-nj-eip.md):
//
//	len == n        one status byte per element (W506 §7-7-4, "Num of Element"
//	                specified)
//	len == 2n       status + forced set/reset per element (scalar BOOL format)
//	len == words*2  bit array, 16 bits per WORD (vendor type 0xFF, what the
//	                Compolet turns a BOOL array into)
//
// Any other length is reported as an error instead of being guessed at.
func (client *Client) omronReadBoolSlice(ctx context.Context, tag string, data []bool) error {
	n := len(data)
	if n == 0 {
		return nil
	}
	words := (n + omronBitsPerWord - 1) / omronBitsPerWord

	raw, err := client.omronReadRaw(ctx, tag, uint16(n))
	if err != nil {
		return err
	}

	switch len(raw) {
	case n:
		// one status byte per element
		for i := 0; i < n; i++ {
			data[i] = raw[i] != 0
		}
		return nil
	case 2 * n:
		// status + forced set/reset per element
		for i := 0; i < n; i++ {
			data[i] = raw[2*i] != 0
		}
		return nil
	case words * 2:
		// bit array: low bit first, 16 bits per word
		vals := make([]uint16, words)
		for i := range vals {
			vals[i] = binary.LittleEndian.Uint16(raw[i*2:])
		}
		invUnpackBoolWords(vals, data)
		return nil
	default:
		return fmt.Errorf("read of BOOL array %s returned %d bytes, which is none of the known layouts (%d bytes for one status byte per element, %d for two bytes per element, %d for a bit array of %d word(s)); read the elements individually",
			tag, len(raw), n, 2*n, words*2, words)
	}
}

// omronReadRaw issues one connected Read for tag with the given element count
// and returns the raw data bytes (everything after the type header). It reuses
// the regular read path's error classification and fragmented read handling.
func (client *Client) omronReadRaw(ctx context.Context, tag string, elements uint16) ([]byte, error) {
	ioi, err := client.newIOI(tag, CIPTypeBOOL)
	if err != nil {
		return nil, fmt.Errorf("problem generating IOI. %w", err)
	}

	reqItems := make([]CIPItem, 2)
	reqItems[0] = newItem(cipItem_ConnectionAddress, &client.OTNetworkConnectionID)
	readMsg := msgCIPConnectedServiceReq{
		SequenceCount: uint16(sequencer()),
		Service:       CIPService_Read,
		PathLength:    byte(len(ioi.Bytes()) / 2),
	}
	reqItems[1] = newItem(cipItem_ConnectedData, readMsg)
	if err := reqItems[1].Serialize(ioi, elements); err != nil {
		return nil, fmt.Errorf("problem serializing ioi: %w", err)
	}
	itemData, err := serializeItems(reqItems)
	if err != nil {
		return nil, err
	}
	_, data, err := client.send_recv_data(ctx, cipCommandSendUnitData, itemData)
	if err != nil {
		return nil, err
	}

	var resultHdr msgCIPResultHeader
	if err := binary.Read(data, binary.LittleEndian, &resultHdr); err != nil {
		return nil, fmt.Errorf("problem reading result header: %w", err)
	}
	items, err := readItems(data)
	if err != nil {
		return nil, fmt.Errorf("problem reading items: %w", err)
	}
	if len(items) != 2 {
		return nil, fmt.Errorf("wrong Number of Items. Expected 2 but got %v", len(items))
	}
	if cipErr, isErrorReply := parseCIPErrorReply(items[1].Data); isErrorReply {
		return nil, fmt.Errorf("read of %s: %w", tag, cipErr)
	}

	var hdr msgCIPReadResultData
	if err := items[1].DeSerialize(&hdr); err != nil {
		return nil, fmt.Errorf("problem reading item 2's header. %w", err)
	}
	payload := items[1].Data[items[1].Pos:]
	if hdr.Status[1] == byte(CIPStatus_PartialTransfer) {
		payload, err = client.readFragmented(ctx, ioi, elements, payload)
		if err != nil {
			return nil, fmt.Errorf("partial transfer read of %s: %w", tag, err)
		}
	} else if hdr.Status[1] != 0 {
		return nil, fmt.Errorf("read of %s returned status %v", tag, CIPStatus(hdr.Status[1]))
	}
	return payload, nil
}
