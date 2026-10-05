// Command displayctl is a small BetterDisplay-style CLI for macOS on Apple
// Silicon: list displays, switch resolutions (including HiDPI modes), and
// control external monitors over DDC/CI.
package main

import (
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"text/tabwriter"

	"displayctl/internal/display"
	"displayctl/internal/login"
	"displayctl/internal/menubar"
)

// AppKit only runs on the main thread, and the menu command hands it the main
// goroutine for good.
func init() { runtime.LockOSThread() }

const usage = `usage: displayctl [-d display] [-permanent] <command> [args]

commands:
  list                   list all displays and whether they are connected
  menu                   run a menu bar app with a toggle per display
  login [on|off]         show or change whether the menu bar app starts at login
  connect                reconnect a display
  disconnect             disconnect a display without unplugging it
  toggle                 connect or disconnect, whichever applies
  modes                  list resolutions for a display
  mode <n | WxH[@Hz]>    switch resolution (n is a number from "modes")
  brightness [v|+v|-v]   get or set brightness over DDC/CI
  contrast [v|+v|-v]     get or set contrast
  volume [v|+v|-v]       get or set speaker volume
  vcp <code> [v]         raw DDC/CI get or set, e.g. "vcp 0x60"

-d picks a display by ID, UUID prefix, or part of its name. The default is the
main display. Connect and disconnect last for this login session; -permanent
makes them survive a reboot.
`

var permanent = flag.Bool("permanent", false, "keep connect/disconnect across reboots")

func main() {
	sel := flag.String("d", "", "display ID, UUID prefix, or part of its name")
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*sel, flag.Arg(0), flag.Args()[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "displayctl:", err)
		os.Exit(1)
	}
}

func run(sel, cmd string, args []string) error {
	displays, err := display.List()
	if err != nil {
		return err
	}
	if cmd == "list" {
		printDisplays(displays)
		return nil
	}
	if cmd == "menu" {
		menubar.Run(menuItems, menuToggle, menubar.Login{State: menuLogin, Toggle: menuLoginToggle})
		return nil
	}
	if cmd == "login" {
		return loginCmd(args)
	}
	d, err := pick(displays, sel)
	if err != nil {
		return err
	}

	switch cmd {
	case "connect":
		return setEnabled(d, true)
	case "disconnect":
		return setEnabled(d, false)
	case "toggle":
		return setEnabled(d, !d.Active)
	case "modes":
		return printModes(d)
	case "mode":
		if len(args) != 1 {
			return errors.New("usage: mode <n | WxH[@Hz]>")
		}
		return setMode(d, args[0])
	case "brightness":
		return vcp(d, display.Brightness, args)
	case "contrast":
		return vcp(d, display.Contrast, args)
	case "volume":
		return vcp(d, display.Volume, args)
	case "vcp":
		if len(args) == 0 {
			return errors.New("usage: vcp <code> [value]")
		}
		code, err := strconv.ParseUint(args[0], 0, 8)
		if err != nil {
			return fmt.Errorf("bad VCP code %q", args[0])
		}
		return vcp(d, display.VCP(code), args[1:])
	}
	return fmt.Errorf("unknown command %q", cmd)
}

// pick finds a display by exact ID, UUID prefix, or part of its name. An
// ambiguous name is an error rather than a guess, since these commands move
// windows around.
func pick(ds []display.Display, sel string) (display.Display, error) {
	if sel == "" {
		for _, d := range ds {
			if d.Main {
				return d, nil
			}
		}
		return display.Display{}, errors.New("no main display; name one with -d")
	}

	var matches []display.Display
	for _, d := range ds {
		switch {
		case sel == strconv.Itoa(int(d.ID)):
			return d, nil
		case d.UUID != "" && strings.HasPrefix(strings.ToLower(d.UUID), strings.ToLower(sel)),
			strings.Contains(strings.ToLower(d.Name), strings.ToLower(sel)):
			matches = append(matches, d)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return display.Display{}, fmt.Errorf("no display matches %q (see \"displayctl list\")", sel)
	default:
		return display.Display{}, fmt.Errorf("%q matches %d displays; use an ID or UUID", sel, len(matches))
	}
}

func printDisplays(ds []display.Display) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSTATUS\tNAME\tMODE\tORIGIN\tUUID\t")
	for _, d := range ds {
		var tags []string
		if d.Main {
			tags = append(tags, "main")
		}
		if d.Builtin {
			tags = append(tags, "built-in")
		}
		if d.MirrorOf != 0 {
			tags = append(tags, fmt.Sprintf("mirrors %d", d.MirrorOf))
		}
		if d.Rotation != 0 {
			tags = append(tags, fmt.Sprintf("rotated %g°", d.Rotation))
		}
		mode, origin := "-", "-"
		if d.Active {
			mode = d.Mode.String()
			origin = fmt.Sprintf("%g,%g", d.X, d.Y)
		}
		uuid, _, _ := strings.Cut(d.UUID, "-")
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
			d.ID, d.Status(), d.Name, mode, origin, uuid, strings.Join(tags, ", "))
	}
	tw.Flush()
	if !display.CanSetEnabled() {
		fmt.Fprintln(os.Stderr, "\nwarning: connect/disconnect is unavailable on this macOS version")
	}
}

// ---- Menu bar --------------------------------------------------------------

// menuItems builds the menu each time it opens: one line per display, checked
// when connected.
func menuItems() []menubar.Item {
	ds, err := display.List()
	if err != nil {
		return []menubar.Item{{Title: err.Error()}}
	}
	active := 0
	for _, d := range ds {
		if d.Active {
			active++
		}
	}

	items := make([]menubar.Item, 0, len(ds))
	for _, d := range ds {
		title := d.Name
		if d.Active {
			title += fmt.Sprintf("  %dx%d", d.Mode.Width, d.Mode.Height)
		}
		items = append(items, menubar.Item{
			ID:    uint32(d.ID),
			Title: title,
			On:    d.Active,
			// The last active display has no toggle: there would be no screen
			// left to turn it back on from.
			Enabled: !d.Active || active > 1,
		})
	}
	return items
}

func menuToggle(id uint32) {
	ds, err := display.List()
	if err != nil {
		fmt.Fprintln(os.Stderr, "displayctl:", err)
		return
	}
	for _, d := range ds {
		if uint32(d.ID) == id {
			if _, err := display.SetEnabled(d, !d.Active, *permanent); err != nil {
				fmt.Fprintln(os.Stderr, "displayctl:", err)
			}
			return
		}
	}
}

// menuLogin shows "Start at Login", disabled when brew services owns it.
func menuLogin() (on, enabled bool, title string) {
	if login.ByBrew() {
		return true, false, "Start at Login (brew services)"
	}
	return login.Enabled(), true, "Start at Login"
}

func menuLoginToggle() {
	var err error
	if login.Enabled() {
		err = login.Disable()
	} else {
		err = login.Enable()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "displayctl:", err)
	}
}

func loginCmd(args []string) error {
	if login.ByBrew() {
		fmt.Println("starts at login via brew services; change it with brew services start|stop displayctl")
		return nil
	}
	switch strings.Join(args, " ") {
	case "":
	case "on":
		if err := login.Enable(); err != nil {
			return err
		}
	case "off":
		if err := login.Disable(); err != nil {
			return err
		}
	default:
		return errors.New("usage: displayctl login [on|off]")
	}
	fmt.Println(map[bool]string{true: "starts at login", false: "does not start at login"}[login.Enabled()])
	return nil
}

// setEnabled connects or disconnects, and says how to undo it, because a
// disconnected display takes its windows with it.
func setEnabled(d display.Display, enable bool) error {
	changed, err := display.SetEnabled(d, enable, *permanent)
	if err != nil {
		return err
	}
	verb := map[bool]string{true: "connected", false: "disconnected"}[enable]
	if !changed {
		fmt.Printf("%s is already %s\n", d.Name, verb)
		return nil
	}
	fmt.Printf("%s %s\n", d.Name, verb)
	if !enable {
		fmt.Printf("undo with: displayctl -d %d connect\n", d.ID)
	}
	return nil
}

func printModes(d display.Display) error {
	modes, err := d.Modes()
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(tw, "#\tLOOKS LIKE\tPIXELS\tREFRESH\t\t")
	for i, m := range modes {
		var tags []string
		if m.HiDPI() {
			tags = append(tags, "HiDPI")
		}
		if m.Current {
			tags = append(tags, "current")
		}
		fmt.Fprintf(tw, "%d\t%dx%d\t%dx%d\t%s\t\t%s\n",
			i+1, m.Width, m.Height, m.PixelWidth, m.PixelHeight, m.RefreshString(), strings.Join(tags, ", "))
	}
	return tw.Flush()
}

func setMode(d display.Display, arg string) error {
	modes, err := d.Modes()
	if err != nil {
		return err
	}
	m, err := findMode(modes, arg)
	if err != nil {
		return err
	}
	if err := display.SetMode(d.ID, m); err != nil {
		return err
	}
	fmt.Println("switched to", m)
	return nil
}

// findMode takes a number from "modes" or WxH[@Hz]. For WxH it relies on the
// sort order from Modes, so HiDPI and the fastest refresh rate win.
func findMode(modes []display.Mode, arg string) (display.Mode, error) {
	if n, err := strconv.Atoi(arg); err == nil {
		if n < 1 || n > len(modes) {
			return display.Mode{}, fmt.Errorf("mode %d out of range 1-%d", n, len(modes))
		}
		return modes[n-1], nil
	}

	size, hz, hasHz := strings.Cut(arg, "@")
	ws, hs, _ := strings.Cut(size, "x")
	w, errW := strconv.Atoi(ws)
	h, errH := strconv.Atoi(hs)
	rate, errR := strconv.ParseFloat(strings.TrimSuffix(strings.ToLower(hz), "hz"), 64)
	if errW != nil || errH != nil || (hasHz && errR != nil) {
		return display.Mode{}, fmt.Errorf("bad mode %q, want a number or WxH[@Hz]", arg)
	}
	for _, m := range modes {
		if m.Width == w && m.Height == h && (!hasHz || math.Abs(m.Refresh-rate) < 0.5) {
			return m, nil
		}
	}
	return display.Mode{}, fmt.Errorf("no %s mode (see \"displayctl modes\")", arg)
}

// vcp gets a DDC/CI value, or sets it. "60" is absolute, "+10" and "-10" are
// relative to the current value and clamped to the monitor's range.
func vcp(d display.Display, code display.VCP, args []string) error {
	ddc, err := display.OpenDDC(d.ID)
	if err != nil {
		return err
	}
	defer ddc.Close()

	if len(args) == 0 {
		cur, maxv, err := ddc.Get(code)
		if err != nil {
			return err
		}
		fmt.Printf("%d/%d\n", cur, maxv)
		return nil
	}

	arg := args[0]
	value, err := strconv.Atoi(arg)
	if err != nil {
		return fmt.Errorf("bad value %q", arg)
	}
	if arg[0] == '+' || arg[0] == '-' {
		cur, maxv, err := ddc.Get(code)
		if err != nil {
			return err
		}
		value = min(max(int(cur)+value, 0), int(maxv))
	}
	if value < 0 || value > math.MaxUint16 {
		return fmt.Errorf("value %d out of range", value)
	}
	if err := ddc.Set(code, uint16(value)); err != nil {
		return err
	}
	fmt.Println(value)
	return nil
}
