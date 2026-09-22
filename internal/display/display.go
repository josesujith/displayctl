// Package display is a Go API over the macOS display stack: CoreGraphics for
// listing displays and switching modes, and IOKit for talking DDC/CI to
// external monitors.
//
// The Apple-specific C lives in bridge.c. The Go files only convert between
// C structs and Go types, and implement anything that is plain logic.
package display

/*
#cgo CFLAGS: -Wall
#cgo LDFLAGS: -framework CoreFoundation -framework CoreGraphics -framework IOKit -framework CoreDisplay -framework ColorSync
#include "bridge.h"
*/
import "C"

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
)

// ID is a CoreGraphics display ID (CGDirectDisplayID). It is stable while a
// display stays connected but can change across reconnects and reboots.
type ID uint32

type Display struct {
	ID       ID
	UUID     string // stable across reboots and replugs, unlike ID
	Name     string
	Vendor   uint32
	Model    uint32
	Serial   uint32
	Builtin  bool
	Main     bool
	Active   bool    // part of the desktop, as opposed to disconnected
	Known    bool    // disconnected and no longer reported by macOS; see state.go
	MirrorOf ID      // 0 unless this display mirrors another
	X, Y     float64 // origin in the global desktop, in points
	Rotation float64 // degrees
	Mode     Mode    // current mode
}

// Status is "on", "off", or "off*" for a display macOS no longer reports.
func (d Display) Status() string {
	switch {
	case d.Active:
		return "on"
	case d.Known:
		return "off*"
	default:
		return "off"
	}
}

type Mode struct {
	Width, Height           int     // logical size in points: what the UI "looks like"
	PixelWidth, PixelHeight int     // framebuffer size in pixels
	Refresh                 float64 // Hz
	Usable                  bool    // usable for the desktop, as opposed to TV modes
	Current                 bool

	ioModeID int32
	ioFlags  uint32
}

// HiDPI reports whether macOS renders this mode at 2x ("Retina" scaling).
func (m Mode) HiDPI() bool { return m.PixelWidth > m.Width }

func (m Mode) RefreshString() string {
	return strconv.FormatFloat(math.Round(m.Refresh*100)/100, 'f', -1, 64) + "Hz"
}

func (m Mode) String() string {
	s := fmt.Sprintf("%dx%d @ %s", m.Width, m.Height, m.RefreshString())
	if m.HiDPI() {
		s += fmt.Sprintf(" HiDPI (%dx%d px)", m.PixelWidth, m.PixelHeight)
	}
	return s
}

// ErrModeNotFound means the requested mode is no longer offered by the display.
var ErrModeNotFound = errors.New("display: mode not available")

// List returns every display macOS reports: active ones, ones that are
// connected but switched off, and any this tool disconnected and macOS has
// since stopped reporting at all.
func List() ([]Display, error) {
	var buf [C.DC_MAX_DISPLAYS]C.dc_display
	n := C.dc_list_displays(&buf[0], C.DC_MAX_DISPLAYS)
	if n < 0 {
		return nil, errors.New("display: cannot list displays")
	}
	out := make([]Display, 0, n)
	seen := make(map[ID]bool, n)
	for _, d := range buf[:n] {
		e := Display{
			ID:       ID(d.id),
			UUID:     C.GoString(&d.uuid[0]),
			Name:     C.GoString(&d.name[0]),
			Vendor:   uint32(d.vendor),
			Model:    uint32(d.model),
			Serial:   uint32(d.serial),
			Builtin:  bool(d.builtin),
			Main:     bool(d.main),
			Active:   bool(d.active),
			MirrorOf: ID(d.mirror_of),
			X:        float64(d.x),
			Y:        float64(d.y),
			Rotation: float64(d.rotation),
			Mode:     goMode(d.mode),
		}
		// WindowServer keeps empty slots around: inactive, anonymous, and
		// nothing you can connect. Leave them out.
		if !e.Active && e.UUID == "" && e.Vendor == 0 && e.Model == 0 && e.Serial == 0 {
			continue
		}
		if e.Name == "" {
			e.Name = defaultName(e)
		}
		seen[e.ID] = true
		out = append(out, e)
	}

	for uuid, g := range loadDisconnected() {
		if !seen[g.ID] {
			out = append(out, Display{ID: g.ID, UUID: uuid, Name: g.Name, Known: true})
		}
	}
	return out, nil
}

func defaultName(d Display) string {
	if d.Builtin {
		return "Built-in Display"
	}
	return fmt.Sprintf("Display %d", d.ID)
}

// Modes returns the display's desktop-usable modes, largest first. Within one
// size, HiDPI comes before low resolution and faster refresh before slower.
func (d Display) Modes() ([]Mode, error) { return Modes(d.ID) }

func Modes(id ID) ([]Mode, error) {
	buf := make([]C.dc_mode, 256)
	n := int(C.dc_list_modes(C.uint32_t(id), &buf[0], C.int(len(buf))))
	if n > len(buf) {
		buf = make([]C.dc_mode, n)
		n = min(n, int(C.dc_list_modes(C.uint32_t(id), &buf[0], C.int(len(buf)))))
	}
	if n < 0 {
		return nil, fmt.Errorf("display: cannot read modes for display %d", id)
	}

	var out []Mode
	for _, cm := range buf[:n] {
		if m := goMode(cm); m.Usable {
			out = append(out, m)
		}
	}
	slices.SortFunc(out, func(a, b Mode) int {
		return cmp.Or(
			cmp.Compare(b.Width, a.Width),
			cmp.Compare(b.Height, a.Height),
			cmp.Compare(b.PixelWidth, a.PixelWidth),
			cmp.Compare(b.Refresh, a.Refresh),
		)
	})
	return out, nil
}

// SetMode switches the display to m, which must come from Modes. The change
// persists across reboots, like picking it in System Settings.
func SetMode(id ID, m Mode) error {
	switch rc := C.dc_set_mode(C.uint32_t(id), cMode(m)); rc {
	case 0:
		return nil
	case C.DC_ERR_NO_MODE:
		return ErrModeNotFound
	default:
		return fmt.Errorf("display: CoreGraphics error %d", rc)
	}
}

func goMode(m C.dc_mode) Mode {
	return Mode{
		Width:       int(m.width),
		Height:      int(m.height),
		PixelWidth:  int(m.pixel_width),
		PixelHeight: int(m.pixel_height),
		Refresh:     float64(m.refresh),
		Usable:      bool(m.usable),
		Current:     bool(m.current),
		ioModeID:    int32(m.io_mode_id),
		ioFlags:     uint32(m.io_flags),
	}
}

func cMode(m Mode) C.dc_mode {
	return C.dc_mode{
		width:        C.uint32_t(m.Width),
		height:       C.uint32_t(m.Height),
		pixel_width:  C.uint32_t(m.PixelWidth),
		pixel_height: C.uint32_t(m.PixelHeight),
		refresh:      C.double(m.Refresh),
		io_mode_id:   C.int32_t(m.ioModeID),
		io_flags:     C.uint32_t(m.ioFlags),
	}
}
