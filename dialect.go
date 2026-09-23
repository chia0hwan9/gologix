package gologix

import "fmt"

// Dialect selects the tag, encoding and data layout semantics used by a Client.
//
// The zero value is DialectLogix, which is byte-for-byte the behavior this
// library had before the dialect switch existed. DialectInovance enables the
// Inovance (汇川) EIP tag semantics described by "EIP 标签通信库使用说明"
// V2.0.2.8, which differ from Rockwell/Allen-Bradley in the STRING/STRUCT type
// codes and in the BOOL/struct alignment rules.
type Dialect int

const (
	// DialectLogix is the default: Rockwell/Allen-Bradley Logix semantics.
	DialectLogix Dialect = iota
	// DialectInovance enables the Inovance EIP tag semantics.
	DialectInovance
)

func (d Dialect) String() string {
	switch d {
	case DialectLogix:
		return "logix"
	case DialectInovance:
		return "inovance"
	default:
		return fmt.Sprintf("dialect(%d)", int(d))
	}
}

// InovanceAlign mirrors the TAG_AlignType parameter of the Inovance EIP tag
// library (documentation §2.4). The alignment is chosen per request on the
// Inovance side; on this side it is a client/connection level option because
// the two modes are explicitly documented as not interchangeable (§7.13).
type InovanceAlign int

const (
	// AlignDefault is AT_DEFAULT (0x00): the EIP default alignment rules.
	//
	// Single BOOL members occupy 2 bytes (2-byte aligned, only the LSB valid),
	// BOOL[n] members are bit packed and padded to a multiple of 16 bits, and
	// the other members follow C alignment rules.
	AlignDefault InovanceAlign = iota

	// AlignInoProShop is AT_INOPROSHOP (0x01): the alignment configured for
	// the struct in InoProShop. Only the "align to 1 byte" setting is
	// implemented, where everything is packed with no padding at all: a BOOL
	// member takes 1 byte and BOOL[n] takes n bytes.
	AlignInoProShop

	// invAlignUnsupported marks the end of the supported range. The InoProShop
	// alignment parameter may also be 2/4/8; those layouts are not documented
	// with examples and are rejected rather than guessed.
	invAlignUnsupported
)

func (a InovanceAlign) String() string {
	switch a {
	case AlignDefault:
		return "default"
	case AlignInoProShop:
		return "inoproshop"
	default:
		return fmt.Sprintf("align(%d)", int(a))
	}
}

// InovanceOptions carries the options used when Client.Dialect is
// DialectInovance. The zero value is AlignDefault with lenient size checking.
type InovanceOptions struct {
	// Align must match the alignment the PLC side is using. The two modes are
	// not interchangeable and the PLC cannot switch back without a Run/Stop
	// restart (documentation §7.13/§7.14).
	Align InovanceAlign

	// StrictSize makes writes fail locally (before the request is sent) when
	// the payload length does not match the expected tag size. Without it the
	// PLC answers with ERRR_WRITE_DATASIZE_UNCONSISTENT instead.
	StrictSize bool

	// KeepTagCase stops the client from lower casing tag paths. Tag names on
	// Inovance controllers are matched against the scanned symbol table, so
	// preserving the original spelling is safer.
	KeepTagCase bool
}

// UseInovance switches the client to the Inovance dialect with the given
// alignment mode.
//
// The alignment must match the PLC side; mixing the two modes on one
// connection is documented as unsupported (§7.13). Changing the alignment on
// the PLC itself requires stopping and restarting it (§7.14).
func (client *Client) UseInovance(align InovanceAlign) error {
	if align < 0 || align >= invAlignUnsupported {
		return fmt.Errorf("unsupported Inovance alignment %d: only AlignDefault (%d) and AlignInoProShop (%d) are implemented",
			int(align), int(AlignDefault), int(AlignInoProShop))
	}
	client.Dialect = DialectInovance
	client.Inovance.Align = align
	return nil
}

// isInovance reports whether the client must use Inovance semantics.
func (client *Client) isInovance() bool {
	return client.Dialect == DialectInovance
}

// inovanceAlign returns the alignment mode in effect for this client.
func (client *Client) inovanceAlign() InovanceAlign {
	if client.Inovance.Align >= 0 && client.Inovance.Align < invAlignUnsupported {
		return client.Inovance.Align
	}
	return AlignDefault
}
