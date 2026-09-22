#!/bin/sh
# Builds DisplayCtl.app, a double-clickable menu bar app wrapping the same
# binary. LSUIElement keeps it out of the Dock and the app switcher.
set -e

cd "$(dirname "$0")"
APP=DisplayCtl.app

go build -o displayctl .

rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS"
# Named displayctl-bin, not displayctl: the launcher below is "DisplayCtl", and
# on a case-insensitive filesystem that is the same name, so one would silently
# overwrite the other.
cp displayctl "$APP/Contents/MacOS/displayctl-bin"

# The bundle launches with no arguments, so a one-line launcher supplies them.
cat > "$APP/Contents/MacOS/DisplayCtl" <<'EOF'
#!/bin/sh
# Launched with no terminal attached, so keep a log to look at when something
# does not appear in the menu bar.
LOG="$HOME/Library/Logs/displayctl.log"
exec "$(dirname "$0")/displayctl-bin" menu >>"$LOG" 2>&1
EOF
chmod +x "$APP/Contents/MacOS/DisplayCtl"

cat > "$APP/Contents/Info.plist" <<'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key><string>DisplayCtl</string>
	<key>CFBundleDisplayName</key><string>DisplayCtl</string>
	<key>CFBundleIdentifier</key><string>local.displayctl</string>
	<key>CFBundleExecutable</key><string>DisplayCtl</string>
	<key>CFBundlePackageType</key><string>APPL</string>
	<key>CFBundleShortVersionString</key><string>0.1</string>
	<key>LSMinimumSystemVersion</key><string>13.0</string>
	<key>LSUIElement</key><true/>
</dict>
</plist>
EOF

echo "built $(pwd)/$APP"
