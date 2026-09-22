package display

/*
#include "bridge.h"
*/
import "C"

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var (
	// ErrLastDisplay guards the one mistake there is no way back from: turning
	// off the only screen you can still read the screen on.
	ErrLastDisplay = errors.New("display: refusing to disconnect the only active display")

	ErrNoSkyLight = errors.New("display: SLSConfigureDisplayEnabled is unavailable; this macOS version may have moved it")
)

// CanSetEnabled reports whether the private connect/disconnect API resolved.
func CanSetEnabled() bool { return bool(C.dc_can_set_enabled()) }

// SetEnabled connects or disconnects a display. The cable stays plugged in;
// macOS drops the display from the desktop and moves its windows away, exactly
// like BetterDisplay's disconnect.
//
// When permanent is false the change lasts for the login session only, so a
// reboot undoes it. It returns false if the display was already in that state.
func SetEnabled(d Display, enabled, permanent bool) (changed bool, err error) {
	if !CanSetEnabled() {
		return false, ErrNoSkyLight
	}
	// WindowServer rejects a no-op transaction, and repeating a command should
	// be harmless.
	if d.Active == enabled && !d.Known {
		remember(d, enabled)
		return false, nil
	}
	if !enabled {
		others, err := List()
		if err != nil {
			return false, err
		}
		active := 0
		for _, o := range others {
			if o.Active {
				active++
			}
		}
		if active <= 1 && d.Active {
			return false, ErrLastDisplay
		}
	}

	if rc := C.dc_set_enabled(C.uint32_t(d.ID), C.bool(enabled), C.bool(permanent)); rc != 0 {
		return false, fmt.Errorf("display: cannot %s display %d: CoreGraphics error %d",
			map[bool]string{true: "connect", false: "disconnect"}[enabled], d.ID, rc)
	}
	remember(d, enabled)
	return true, nil
}

// ---- Remembering disconnected displays -------------------------------------
//
// Once disconnected, macOS may stop listing a display altogether, which would
// leave no ID to switch it back on with. So they are written to disk, keyed by
// the UUID, which survives reboots and replugs.

type disconnected struct {
	ID   ID     `json:"id"`
	Name string `json:"name"`
}

func stateFile() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "displayctl", "disconnected.json")
}

func loadDisconnected() map[string]disconnected {
	out := map[string]disconnected{}
	path := stateFile()
	if path == "" {
		return out
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]disconnected{}
	}
	return out
}

func remember(d Display, enabled bool) {
	state := loadDisconnected()
	if enabled {
		// Purge by ID too: a blind reconnect by raw ID has no UUID to key on.
		delete(state, d.UUID)
		for uuid, g := range state {
			if g.ID == d.ID {
				delete(state, uuid)
			}
		}
	} else {
		key := d.UUID
		if key == "" {
			key = fmt.Sprintf("id:%d", d.ID)
		}
		state[key] = disconnected{ID: d.ID, Name: d.Name}
	}

	path := stateFile()
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	if data, err := json.MarshalIndent(state, "", "  "); err == nil {
		os.WriteFile(path, data, 0o644)
	}
}
