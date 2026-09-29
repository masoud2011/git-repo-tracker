# Developer FAQ

Common development gotchas and the shortest correct answer for each.

## GNOME extension

### Why does `task gnome:restart` update the installed files, but GNOME still shows old behavior?

GNOME Shell can keep the old extension code loaded in memory even after the files on disk were replaced.

Use:

```sh
gnome-extensions disable git-repo-tracker@laszukdawid.github.com
gnome-extensions enable git-repo-tracker@laszukdawid.github.com
```

If the running Shell still keeps stale code, log out/in. On Wayland that is the reliable full reload path.

### Is nested GNOME Shell enough to test the extension?

Only for the Shell UI itself.

Nested GNOME is good for:

- panel menu layout
- CSS
- submenu behavior
- click handlers that stay inside GNOME Shell

Nested GNOME is not reliable for:

- windows opened by external GUI processes
- the Fyne Settings window surfaced by the extension

Reason: the extension asks the running tray app to show Settings. In nested sessions that GUI window still belongs to the real desktop session that owns the tray app, so it may appear on the outer session rather than inside the nested Shell.

### How should I test the GNOME Settings button, then?

Split the workflow:

1. Use nested GNOME Shell to test extension menu/layout changes.
2. Use the real desktop session to test the Settings button and the Fyne window.

Direct Fyne settings smoke test:

```sh
~/.local/bin/git-repo-tracker --settings
```

Real-session extension reload without rebooting:

```sh
gnome-extensions disable git-repo-tracker@laszukdawid.github.com
gnome-extensions enable git-repo-tracker@laszukdawid.github.com
```

### Which binaries does the GNOME extension use?

By default it looks for:

```text
~/.local/bin/git-repo-tracker-cli
~/.local/bin/git-repo-tracker
```

`task gnome:install` should rebuild/install those for development.

Override them with:

```text
GIT_REPO_TRACKER_CLI=/absolute/path/to/git-repo-tracker-cli
GIT_REPO_TRACKER_APP=/absolute/path/to/git-repo-tracker
```

Those environment variables must be visible to GNOME Shell's session.

### Why did the custom GNOME panel SVG icon not show up, even after reinstall/restart?

Do not assume a bundled file-backed SVG will render reliably in the GNOME top bar just because the extension code loaded.

What failed here:

- the extension code was refreshing correctly
- the panel slot existed
- the custom SVG icon still rendered as nothing

For GNOME panel work, prefer the simplest visible primitive first:

- a Shell-rendered glyph in `St.Label`
- or a stock symbolic icon

Only move to a custom file-backed icon if there is a strong reason and it is visually verified in GNOME Shell.

Practical rule: if a custom panel icon is invisible, switch to a text/glyph-based mark before debugging more complex asset-loading paths.

## macOS builds and startup

### Why does a rebuild leave the installed app or Login Item on an older version?

Raw `go build` only writes a binary. Use `build/build.sh` or `task build` for a
local delivery: it rebuilds `dist/git-repo-tracker.app`, signs and verifies it,
and replaces `/Applications/git-repo-tracker.app`. `task bundle` and
`task app-build-local` use the same path. `task run` also builds first and opens
the installed bundle. Packaging a release without `--install` does not install
anything on the build machine.

The installed path remains stable for the login item. Do not enable startup
from a temporary `go run` or preview executable: that file can disappear.
Use the installed app's Settings to enable **Start git-repo-tracker at login**.

An already running app still uses the old process until you quit and reopen it.
Builds deliberately do not stop or start the app.

To use another installation directory, set `GIT_REPO_TRACKER_INSTALL_DIR` and
enable startup again from that copy. Installation stages and verifies the new
bundle before moving the previous copy, and restores the previous copy if the
final move fails. Locks prevent overlapping local builds, packaging, and
installations. A forcibly killed process can leave `dist/.local-build-lock`,
`dist/.git-repo-tracker.package-lock`, or `.git-repo-tracker.install-lock` in the
installation directory. Confirm no build/installer is running before removing
a leftover empty lock folder and retrying. If restoration fails, the error
prints the retained backup path.

## macOS privacy

### Why does macOS keep asking for Documents access?

The scanner walks every configured root periodically. A `~/` root therefore reaches
`~/Documents` even when the user only intended to track a narrower project folder.

Use the native folder picker in Settings to add the specific project directory.
For a machine that has retained a bad permission decision, remove the broad root
and reset its TCC record before relaunching:

```sh
tccutil reset SystemPolicyDocumentsFolder com.github.laszukdawid.git-repo-tracker
```

The macOS app bundle must retain `NSDocumentsFolderUsageDescription` in
`build/macos/Info.plist`; it explains the request in the system prompt.

## Fyne UI

### Why do custom-painted surfaces and stock Fyne widgets sometimes look like mixed themes?

Because this app uses both:

- Fyne theme colors for stock widgets
- custom palette colors baked into canvas objects

When changing appearance behavior, keep those two paths synchronized. If the app follows system theme changes, both the Fyne theme and the custom palette must resolve to the same concrete light/dark variant.
