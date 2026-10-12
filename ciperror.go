package gologix

import (
	"encoding/binary"
	"errors"
)

// This file adds typed classification for CIP *error replies*.
//
// A CIP error reply is a perfectly well-formed reply: the TCP connection and
// the EIP session stay usable after one, so callers must not treat it as a
// reason to reconnect. The trouble was that such a reply stops after the status
// header - no data type code, no data - so deserializing it as a successful
// read result failed with io.ErrUnexpectedEOF and surfaced as
// "problem reading item 2's header. unexpected EOF". An application level
// problem ("this tag does not exist") therefore looked exactly like a broken
// connection.
//
// The reply is classified from the raw bytes before any deserialization, and
// reported as *CIPError (errors.go) so that errors.Is can tell the two classes
// apart.

var (
	// ErrCIPPathNotFound is general status 0x05. Typical causes: the tag does
	// not exist, a typo in the tag name, or a missing struct member / array
	// element in the tag path.
	ErrCIPPathNotFound = errors.New("path destination unknown (tag not found?)")

	// ErrCIPServiceNotSupported is general status 0x08: the controller does not
	// implement the requested service for that object.
	ErrCIPServiceNotSupported = errors.New("service not supported")

	// ErrCIPNotEnoughData is general status 0x13: the controller does not have
	// enough data at the requested path to satisfy the request.
	ErrCIPNotEnoughData = errors.New("not enough data to satisfy the request")
)

// sentinelForStatus maps the general statuses worth special casing to the
// sentinel errors above, and returns nil for everything else.
func sentinelForStatus(s CIPStatus) error {
	switch s {
	case CIPStatus_PathDestinationUnknown:
		return ErrCIPPathNotFound
	case CIPStatus_ServiceNotSupported:
		return ErrCIPServiceNotSupported
	case CIPStatus_NotEnoughData:
		return ErrCIPNotEnoughData
	default:
		return nil
	}
}

// IsTagNotFound reports whether err was caused by the PLC rejecting the request
// because the requested tag or path does not exist. Prefer this over matching
// on error strings or on io.ErrUnexpectedEOF.
func IsTagNotFound(err error) bool {
	return errors.Is(err, ErrCIPPathNotFound)
}

// Offsets into a connected-mode reply item payload. The layout is fixed by the
// library's own server and by the test fixtures (read_partial_test.go
// buildConnectedReply) and matches msgCIPReadResultData:
//
//	[0:2] sequence counter
//	[2]   service code, reply bit (0x80) set
//	[3]   reserved
//	[4]   general status
//	[5]   additional status size (word count)
//	[6:]  additional status words, then (success replies only) type + data
const (
	cipReplyServiceByte   = 2
	cipReplyStatusByte    = 4
	cipReplyExtSizeByte   = 5
	cipReplyExtStatusByte = 6
)

// parseCIPErrorReply inspects the raw bytes of a connected-mode reply item and
// reports whether they encode a CIP error reply rather than a successful read
// result.
//
// It only looks at the fixed header bytes, so it works for both short error
// replies (no additional status) and longer ones that carry additional status
// words. When an additional status word is present it is decoded into
// CIPError.Extended, which is what the Logix style 0x1F/0x20 vendor statuses
// use.
func parseCIPErrorReply(b []byte) (*CIPError, bool) {
	if len(b) <= cipReplyStatusByte {
		return nil, false
	}

	svc := b[cipReplyServiceByte]
	// A valid CIP response has the reply bit set. When it is not set the buffer
	// does not start with a reply at all, so leave it to the normal deserialize
	// path: genuinely corrupt frames must keep surfacing as transport errors.
	if svc&0x80 == 0 {
		return nil, false
	}

	status := CIPStatus(b[cipReplyStatusByte])
	// 0x00 is success. 0x06 (PartialTransfer) is a success-shaped reply that
	// still carries the type header and data; ReadSingleWithContext follows it
	// up with fragmented reads, so it must never be classified as an error.
	if status == CIPStatus_OK || status == CIPStatus_PartialTransfer {
		return nil, false
	}

	cipErr := &CIPError{Code: byte(status)}
	if len(b) >= cipReplyExtStatusByte+2 && b[cipReplyExtSizeByte] > 0 {
		cipErr.Extended = binary.LittleEndian.Uint16(b[cipReplyExtStatusByte:])
	}
	return cipErr, true
}
