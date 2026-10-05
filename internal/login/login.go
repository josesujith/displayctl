// Package login starts the menu bar app at login, with a launchd agent in
// ~/Library/LaunchAgents. launchd reads that directory at every login, so
// writing or removing the file is all it takes; nothing is loaded now, which
// would start a second copy alongside the one already running.
package login

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Label is shared with install.sh, so both manage the same agent.
const Label = "local.displayctl"

// brewLabels are the agents `brew services start displayctl` writes, under the
// current and the older naming.
var brewLabels = []string{"sh.brew.displayctl", "homebrew.mxcl.displayctl"}

func agentPath(label string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist")
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ByBrew reports whether `brew services` starts displayctl at login. It owns
// that agent, so it is left to brew rather than changed from here.
func ByBrew() bool {
	for _, l := range brewLabels {
		if exists(agentPath(l)) {
			return true
		}
	}
	return false
}

// Enabled reports whether displayctl starts at login, either way.
func Enabled() bool {
	return exists(agentPath(Label)) || ByBrew()
}

// Enable starts the running binary's menu bar app at every login.
func Enable() error {
	if ByBrew() {
		return nil
	}
	// Not resolved through symlinks: Homebrew's /opt/homebrew/opt and bin
	// links survive upgrades, the versioned Cellar path they point to does not.
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// Inside DisplayCtl.app, start the bundle's launcher, which keeps a log.
	args := []string{exe, "menu"}
	if dir := filepath.Dir(exe); strings.HasSuffix(dir, ".app/Contents/MacOS") {
		if launcher := filepath.Join(dir, "DisplayCtl"); exists(launcher) {
			args = []string{launcher}
		}
	}

	home, _ := os.UserHomeDir()
	logPath := filepath.Join(home, "Library", "Logs", "displayctl.log")
	var b bytes.Buffer
	fmt.Fprintf(&b, `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>%s</string>
	<key>ProgramArguments</key>
	<array>
`, Label)
	for _, a := range args {
		fmt.Fprintf(&b, "\t\t<string>%s</string>\n", xmlEscape(a))
	}
	// RunAtLoad without KeepAlive: it starts at login, and Quit in the menu
	// stays quit instead of being restarted straight away.
	fmt.Fprintf(&b, `	</array>
	<key>RunAtLoad</key><true/>
	<key>ProcessType</key><string>Interactive</string>
	<key>StandardOutPath</key><string>%[1]s</string>
	<key>StandardErrorPath</key><string>%[1]s</string>
</dict>
</plist>
`, xmlEscape(logPath))

	path := agentPath(Label)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b.Bytes(), 0o644)
}

// Disable stops displayctl starting at login. It leaves the running copy
// alone, and leaves a brew services agent to brew.
func Disable() error {
	if err := os.Remove(agentPath(Label)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func xmlEscape(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}
