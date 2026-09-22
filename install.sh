#!/bin/sh
# Installs DisplayCtl.app into ~/Applications and starts it at login, so the
# menu bar icon survives closing the terminal, quitting, and rebooting.
#
# To remove:
#   launchctl bootout gui/$(id -u)/local.displayctl
#   rm ~/Library/LaunchAgents/local.displayctl.plist
#   rm -rf ~/Applications/DisplayCtl.app
set -e

cd "$(dirname "$0")"
LABEL=local.displayctl
DEST="$HOME/Applications/DisplayCtl.app"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"

# Stop whatever is running before replacing it.
launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
pkill -f 'DisplayCtl.app/Contents/MacOS/displayctl-bin' 2>/dev/null || true

mkdir -p "$HOME/Library/LaunchAgents"
if [ -d /Applications/DisplayCtl.app ]; then
	# Installed by the Homebrew cask: use that copy rather than a second one.
	DEST=/Applications/DisplayCtl.app
	echo "using the installed $DEST"
else
	./make-app.sh >/dev/null
	mkdir -p "$HOME/Applications"
	rm -rf "$DEST"
	cp -R DisplayCtl.app "$DEST"
fi

# RunAtLoad without KeepAlive: it starts at login, and Quit in the menu stays
# quit instead of being restarted straight away.
cat > "$PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>$LABEL</string>
	<key>ProgramArguments</key>
	<array>
		<string>$DEST/Contents/MacOS/DisplayCtl</string>
	</array>
	<key>RunAtLoad</key><true/>
	<key>ProcessType</key><string>Interactive</string>
</dict>
</plist>
EOF

launchctl bootstrap "gui/$(id -u)" "$PLIST"

echo "installed $DEST"
echo "starts at login; quit from the menu, restart with:"
echo "  launchctl kickstart -k gui/$(id -u)/$LABEL"
