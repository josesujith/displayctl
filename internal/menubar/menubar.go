// Package menubar puts a status item in the macOS menu bar. The menu is
// rebuilt every time it opens, so displays that are plugged in or unplugged
// while it runs show up without a restart.
package menubar

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
#include "menubar.h"
*/
import "C"

import "unsafe"

// Item is one line in the menu: a display, with a checkmark when connected.
type Item struct {
	ID      uint32
	Title   string
	On      bool
	Enabled bool
}

// Login is the "Start at Login" checkbox at the foot of the menu.
type Login struct {
	State  func() (on, enabled bool, title string)
	Toggle func()
}

var (
	itemsFn  func() []Item
	toggleFn func(id uint32)
	loginCfg Login
)

// Run shows the status item and runs the AppKit event loop. It does not
// return. Call it from the main goroutine with the thread locked, since AppKit
// insists on the main thread.
func Run(items func() []Item, toggle func(id uint32), login Login) {
	itemsFn, toggleFn, loginCfg = items, toggle, login
	C.mb_run()
}

//export menubarRebuild
func menubarRebuild() {
	if itemsFn == nil {
		return
	}
	for _, it := range itemsFn() {
		title := C.CString(it.Title)
		C.mb_add_item(title, C.uint32_t(it.ID), C.bool(it.On), C.bool(it.Enabled))
		C.free(unsafe.Pointer(title))
	}
}

//export menubarToggle
func menubarToggle(id C.uint32_t) {
	if toggleFn != nil {
		toggleFn(uint32(id))
	}
}

//export menubarLogin
func menubarLogin() {
	if loginCfg.State == nil {
		return
	}
	on, enabled, title := loginCfg.State()
	t := C.CString(title)
	C.mb_add_login(t, C.bool(on), C.bool(enabled))
	C.free(unsafe.Pointer(t))
}

//export menubarLoginToggle
func menubarLoginToggle() {
	if loginCfg.Toggle != nil {
		loginCfg.Toggle()
	}
}
