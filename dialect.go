package gologix

import (
	"bytes"
	"fmt"
)

// Dialect selects the tag, encoding and data layout semantics used by a Client.
//
// The zero value is DialectLogix, which is byte-for-byte the behavior this
// library had before the dialect switch existed. DialectInovance enables the
// Inovance (汇川) EIP tag semantics described by "EIP 标签通信库使用说明"
// V2.0.2.8, which differ from Rockwell/Allen-Bradley in the STRING/STRUCT type
// codes and in the BOOL/struct alignment rules. DialectOmron enables the
// OMRON NJ/NX semantics described by the W506 manual and confirmed against the
// OMRON Compolet assemblies (see docs/omron-nx-nj-eip.md).
type Dialect int

const (
	// DialectLogix is the default: Rockwell/Allen-Bradley Logix semantics.
	DialectLogix Dialect = iota
	// DialectInovance enables the Inovance EIP tag semantics.
	DialectInovance
	// DialectOmron enables the OMRON NJ/NX EIP tag semantics.
	DialectOmron
)

func (d Dialect) String() string {
	switch d {
	case DialectLogix:
		return "logix"
	case DialectInovance:
		return "inovance"
	case DialectOmron:
		return "omron"
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
// DialectInovance. The zero value is AlignDefault with the historical tag path
// handling (tag names are lower cased).
type InovanceOptions struct {
	// Align must match the alignment the PLC side is using. The two modes are
	// not interchangeable and the PLC cannot switch back without a Run/Stop
	// restart (documentation §7.13/§7.14).
	Align InovanceAlign

	// KeepTagCase stops the client from lower casing tag paths, which is what
	// the library has always done (see newIOI). Inovance controllers resolve
	// tags against their scanned symbol table, so if a site turns out to be
	// case sensitive, set this to true.
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

// OmronOptions carries the options used when Client.Dialect is DialectOmron.
// The zero value is the recommended configuration.
type OmronOptions struct {
	// ConnectionSize overrides the Large_Forward_Open size. Zero means the
	// NJ/NX default of 1994 bytes (an NX701 would allow 8192). The value must
	// stay at or below what the controller accepts, otherwise Forward_Open
	// fails.
	ConnectionSize uint16

	// KeepTagCase is accepted for symmetry with InovanceOptions but has no
	// effect: the Omron dialect always sends the tag name as written, because
	// Sysmac Studio variable names are case sensitive.
	KeepTagCase bool
}

// UseOmron switches the client to the OMRON NJ/NX dialect.
//
// It also applies the two connection level differences from Rockwell:
//
//   - the connection path is cleared, because an NJ/NX has no backplane/slot
//     and Forward_Open must address the message router directly;
//   - ConnectionSize is lowered to 1994 bytes, the NJ/NX Large_Forward_Open
//     limit (an NX701 allows 8192; use UseOmronWithOptions for that).
//
// Call it before Connect. A ConnectionSize set afterwards is left alone.
func (client *Client) UseOmron() error {
	return client.UseOmronWithOptions(OmronOptions{})
}

// UseOmronWithOptions is UseOmron with explicit options.
func (client *Client) UseOmronWithOptions(opts OmronOptions) error {
	client.Dialect = DialectOmron
	client.Omron = opts

	// No backplane: the Forward_Open connection path must be empty so that it
	// addresses the message router (20 02 24 01) directly.
	client.Controller.Path = &bytes.Buffer{}

	if opts.ConnectionSize > 0 {
		client.ConnectionSize = opts.ConnectionSize
	} else {
		client.ConnectionSize = omronConnectionSize
	}
	return nil
}

// isInovance reports whether the client must use Inovance semantics.
func (client *Client) isInovance() bool {
	return client.Dialect == DialectInovance
}

// isOmron reports whether the client must use OMRON NJ/NX semantics.
func (client *Client) isOmron() bool {
	return client.Dialect == DialectOmron
}

// inovanceAlign returns the alignment mode in effect for this client.
func (client *Client) inovanceAlign() InovanceAlign {
	if client.Inovance.Align >= 0 && client.Inovance.Align < invAlignUnsupported {
		return client.Inovance.Align
	}
	return AlignDefault
}

// keepTagCase reports whether tag paths must keep their original spelling.
// The default (false) preserves the library's historical behaviour of lower
// casing every tag path before building an IOI.
func (client *Client) keepTagCase() bool {
	if client.isOmron() {
		// Sysmac Studio variable names are case sensitive.
		return true
	}
	return client.Dialect == DialectInovance && client.Inovance.KeepTagCase
}
