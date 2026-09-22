package display

/*
#include "bridge.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// VCP is a DDC/CI "Virtual Control Panel" feature code, as defined by the
// VESA MCCS spec. Monitors support different subsets.
type VCP uint8

const (
	Brightness VCP = 0x10
	Contrast   VCP = 0x12
	Input      VCP = 0x60
	Volume     VCP = 0x62
	Mute       VCP = 0x8D
	Power      VCP = 0xD6
)

// DDC/CI framing. The host talks to the monitor at I²C address 0x37, and every
// message starts with the host's source address 0x51, which IOAVService takes
// as a separate sub-address argument rather than as part of the buffer.
const (
	subAddr   = 0x51
	destAddr  = 0x6E // 0x37<<1; only appears in checksums
	replyAddr = 0x50 // virtual host address that seeds reply checksums

	writeGap  = 10 * time.Millisecond
	replyWait = 50 * time.Millisecond // the spec asks for at least 40ms
	attempts  = 4
)

var (
	ErrNoDDC       = errors.New("display: no DDC/CI channel (built-in, AirPlay, or unsupported port)")
	ErrUnsupported = errors.New("display: monitor does not support this VCP code")
	errClosed      = errors.New("display: DDC channel closed")
)

// DDC is an open DDC/CI channel to one external monitor. Monitors are slow and
// easily confused by overlapping commands, so calls are serialized.
type DDC struct {
	mu sync.Mutex
	h  *C.dc_ddc
}

func OpenDDC(id ID) (*DDC, error) {
	h := C.dc_ddc_open(C.uint32_t(id))
	if h == nil {
		return nil, ErrNoDDC
	}
	return &DDC{h: h}, nil
}

func (d *DDC) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.h != nil {
		C.dc_ddc_close(d.h)
		d.h = nil
	}
	return nil
}

// Get reads a feature's current and maximum values.
func (d *DDC) Get(code VCP) (value, maximum uint16, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	req := frame(0x01, byte(code)) // Get VCP Feature
	for range attempts {
		if err = d.write(req); err != nil {
			continue
		}
		time.Sleep(replyWait)
		reply := make([]byte, 11)
		if err = d.read(reply); err != nil {
			continue
		}
		if value, maximum, err = parseReply(code, reply); err == nil || errors.Is(err, ErrUnsupported) {
			return value, maximum, err
		}
	}
	return 0, 0, fmt.Errorf("%w (DDC/CI may be off in the monitor's menu, or unsupported, e.g. G-SYNC module monitors)", err)
}

// Set writes a feature. Monitors don't acknowledge writes, so a nil error only
// means the bytes went out.
func (d *DDC) Set(code VCP, value uint16) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	msg := frame(0x03, byte(code), byte(value>>8), byte(value)) // Set VCP Feature
	// Some monitors drop the first write after being idle. m1ddc and
	// MonitorControl send every write twice for the same reason.
	for range 2 {
		time.Sleep(writeGap)
		if err := d.write(msg); err != nil {
			return err
		}
	}
	return nil
}

func (d *DDC) write(b []byte) error {
	if d.h == nil {
		return errClosed
	}
	if rc := C.dc_ddc_write(d.h, subAddr, (*C.uint8_t)(&b[0]), C.uint32_t(len(b))); rc != 0 {
		return fmt.Errorf("display: DDC write failed (IOReturn %#x)", uint32(rc))
	}
	return nil
}

func (d *DDC) read(b []byte) error {
	if d.h == nil {
		return errClosed
	}
	if rc := C.dc_ddc_read(d.h, subAddr, (*C.uint8_t)(&b[0]), C.uint32_t(len(b))); rc != 0 {
		return fmt.Errorf("display: DDC read failed (IOReturn %#x)", uint32(rc))
	}
	return nil
}

// frame builds a DDC/CI message: a length byte, the payload, and an XOR
// checksum over the whole message including both addresses.
func frame(payload ...byte) []byte {
	msg := append([]byte{0x80 | byte(len(payload))}, payload...)
	chk := byte(destAddr ^ subAddr)
	for _, b := range msg {
		chk ^= b
	}
	return append(msg, chk)
}

// parseReply decodes a "Get VCP Feature Reply":
//
//	6E 88 02 <result> <code> <type> <max hi> <max lo> <cur hi> <cur lo> <checksum>
func parseReply(code VCP, r []byte) (value, maximum uint16, err error) {
	if len(r) < 11 {
		return 0, 0, fmt.Errorf("display: short DDC reply % x", r)
	}
	chk := byte(replyAddr)
	for _, b := range r[:10] {
		chk ^= b
	}
	switch {
	case chk != r[10]:
		return 0, 0, fmt.Errorf("display: bad DDC reply checksum: % x", r)
	case r[2] != 0x02 || r[4] != byte(code):
		return 0, 0, fmt.Errorf("display: unexpected DDC reply: % x", r)
	case r[3] != 0:
		return 0, 0, ErrUnsupported
	}
	return uint16(r[8])<<8 | uint16(r[9]), uint16(r[6])<<8 | uint16(r[7]), nil
}
