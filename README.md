# displayctl

A BetterDisplay-style display tool for macOS on Apple Silicon, written in Go.
All the Apple-specific code sits in one C file (`internal/display/bridge.c`);
everything else is Go.

```
go build -o displayctl .
```

## Commands

```
displayctl list                    all displays, connected or not
displayctl menu                    menu bar app: one toggle per display
displayctl -d dell disconnect      drop a display from the desktop
displayctl -d dell connect         bring it back
displayctl -d dell toggle          whichever applies
displayctl modes                   every resolution, HiDPI ones included
displayctl mode 13                 switch resolution
displayctl brightness 60           monitor brightness over DDC/CI
```

`-d` takes a display ID, a UUID prefix, or part of a name. Without it,
commands act on the main display.

## Menu bar

`displayctl menu` puts a display icon in the menu bar with one line per
display, checked when connected. Clicking a line toggles it. The menu is
rebuilt each time it opens, so displays that come and go appear on their own.
The last connected display is greyed out, since turning it off would leave no
screen to turn it back on from.

Three ways to run it, in order of how long it sticks around:

```
./displayctl menu                     # foreground; dies with the terminal
./make-app.sh && open DisplayCtl.app  # app bundle; outlives the terminal
./install.sh                          # installs to ~/Applications, starts at login
```

A display icon appears at the right of the menu bar, with no Dock icon. Quit
from the menu itself.

**Start at Login** in the menu, or `displayctl login on|off`, adds or removes
the same launch agent `install.sh` uses (below). It takes effect from the next
login. When `brew services` already starts displayctl, the item shows that and
is left to brew.

`install.sh` copies the bundle to `~/Applications` and adds a launch agent at
`~/Library/LaunchAgents/local.displayctl.plist`. It has `RunAtLoad` but no
`KeepAlive`, so Quit stays quit until the next login. Re-run it after changing
the code. To start it again without logging out:

```
launchctl kickstart -k gui/$(id -u)/local.displayctl
```

To remove it completely:

```
launchctl bootout gui/$(id -u)/local.displayctl
rm ~/Library/LaunchAgents/local.displayctl.plist
rm -rf ~/Applications/DisplayCtl.app
```

The app logs to `~/Library/Logs/displayctl.log`, which is where to look when it
runs with no terminal attached.

## Installing with Homebrew

`Casks/displayctl.rb` installs the app into `/Applications` and the `displayctl`
CLI into the Homebrew prefix:

```
brew install --cask josesujith/tap/displayctl
xattr -dr com.apple.quarantine /Applications/DisplayCtl.app
```

Use the full name, `josesujith/tap/displayctl`. Homebrew 7 refuses to load
anything from a tap you have not trusted when you ask for it by its short name;
installing it by its full name trusts it, and taps the repo if needed.

The `xattr` line is needed because the build is ad-hoc signed rather than
notarized, so Gatekeeper refuses to run it while Homebrew's quarantine flag is
set — the symptom is a "could not verify ... free of malware" dialog, or the
CLI dying instantly with exit 137. Homebrew 7 removed the `--no-quarantine`
option, so clearing the flag afterwards is the way. Signing with a Developer ID
and notarizing removes the need for it, and needs a paid Apple Developer
account.

`Formula/displayctl.rb` builds from source instead and offers
`brew services start displayctl`, with no app bundle and no Gatekeeper
exception. It needs a machine whose Xcode is no older than its macOS, since
Homebrew refuses source builds otherwise: updating the Command Line Tools is
not enough, it checks `Xcode.app` too.

After a cask install, `./install.sh` reuses `/Applications/DisplayCtl.app`
rather than making a second copy.

## Releasing

`./publish.sh 0.1.0` builds the app, zips it, tags, pushes, creates the GitHub
release with the zip attached, and updates the cask with the new version and
checksum. It needs the GitHub CLI (`brew install gh && gh auth login`), and
updates `../homebrew-tap` too when that repo is checked out beside this one.

## Connect and disconnect

This is the same trick BetterDisplay uses: the cable stays plugged in, but
macOS drops the display from the desktop and moves its windows elsewhere.

Changes last for the login session. `-permanent` keeps them across reboots,
which is worth avoiding until you trust the tool.

**If you end up with a black screen:** log out and back in, or reboot, unless
you used `-permanent`. The tool refuses to disconnect your only active display,
so this should not happen.

Disconnected displays can vanish from macOS's display list entirely, which
would leave no ID to switch them back on with. They are recorded in
`~/Library/Application Support/displayctl/disconnected.json` and still appear
in `list` with status `off*`. Reconnect by raw ID works even with no state
file: `displayctl -d 1 connect`.

## Notes and limits

- **Apple Silicon only.** Intel Macs use different APIs for DDC.
- **Private APIs.** Connect/disconnect uses `SLSConfigureDisplayEnabled` from
  SkyLight, looked up at runtime. Display names and DDC use private symbols
  too. Any macOS update can break these, and no App Store build is possible.
- **Other display apps fight back.** If BetterDisplay, Lunar or displayplacer
  is running and manages the same display, it may undo a change within
  seconds. Check with `betterdisplaycli get --name="..." --connected`.
- **DDC needs a monitor that supports it.** Monitors with an NVIDIA G-SYNC
  module generally do not.
- **Unsigned.** Released builds are ad-hoc signed, not notarized, so macOS
  warns about them until they are cleared of quarantine.

## Credits

The Apple APIs here are undocumented, and these projects worked them out
first. All are MIT licensed; the code in this repository is a separate Go
implementation.

- [m1ddc](https://github.com/waydabber/m1ddc) — DDC/CI over `IOAVService`,
  including the packet layout and the timings monitors need
- [MonitorControl](https://github.com/MonitorControl/MonitorControl) — DDC
  retry behaviour
- [dispctl](https://github.com/Forger7/dispctl) — connect and disconnect via
  `SLSConfigureDisplayEnabled`, and the idea of remembering disconnected
  displays so they can be switched back on

## License

MIT. See [LICENSE](LICENSE).
