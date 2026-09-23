package gologix

import (
	"context"
	"fmt"
)

// invReadBoolSlice reads a BOOL[n] tag or a BOOL[n] struct member into data.
//
// Two wire shapes exist (documentation V2.0.2.8):
//
//	§4.2      top level BOOL[n]  n elements come back as n bytes, LSB valid
//	§4.4.2(2) BOOL[n] member     bit packed, rounded up to a multiple of 16 bits
//
// AlignInoProShop (alignment parameter 1) turns both into one byte per element.
func (client *Client) invReadBoolSlice(ctx context.Context, tag string, data []bool) error {
	n := len(data)
	if n == 0 {
		return nil
	}
	if client.inovanceAlign() == AlignInoProShop || !invIsMemberTag(tag) {
		return client.invReadBoolBytes(ctx, tag, data)
	}
	return client.invReadBoolWords(ctx, tag, data)
}

// invReadBoolBytes reads len(data) BOOL elements, one byte each.
func (client *Client) invReadBoolBytes(ctx context.Context, tag string, data []bool) error {
	if len(data) == 1 {
		v, err := read[bool](ctx, client, tag)
		if err != nil {
			return err
		}
		data[0] = v
		return nil
	}

	vals, err := readArray[bool](ctx, client, tag, uint16(len(data)))
	if err != nil {
		return err
	}
	if len(vals) != len(data) {
		return fmt.Errorf("read of %s returned %d bools, want %d", tag, len(vals), len(data))
	}
	copy(data, vals)
	return nil
}

// invReadBoolWords reads a bit packed BOOL[n] member as ceil(n/16) words.
func (client *Client) invReadBoolWords(ctx context.Context, tag string, data []bool) error {
	words := (len(data) + 15) / 16

	if words == 1 {
		// a single element response is returned as an atomic value rather than
		// a list
		v, err := read[uint16](ctx, client, tag)
		if err != nil {
			return err
		}
		invUnpackBoolWords([]uint16{v}, data)
		return nil
	}

	vals, err := readArray[uint16](ctx, client, tag, uint16(words))
	if err != nil {
		return err
	}
	if len(vals) != words {
		return fmt.Errorf("read of %s returned %d words, want %d", tag, len(vals), words)
	}
	invUnpackBoolWords(vals, data)
	return nil
}

// invUnpackBoolWords expands bit packed words (low bit first) into data,
// ignoring the padding bits past the end of data.
func invUnpackBoolWords(words []uint16, data []bool) {
	for w, word := range words {
		for i := 0; i < 16; i++ {
			bit := w*16 + i
			if bit >= len(data) {
				return
			}
			data[bit] = word&(1<<uint(i)) != 0
		}
	}
}
