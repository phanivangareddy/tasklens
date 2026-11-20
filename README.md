# TaskLens

A task manager for **Windows, macOS and Linux**. It shows every running process,
how long it has been running, how much processor time and memory it uses, and
explains in plain words what is slowing the computer down. You can **Pause**,
**Resume**, **End** or **Force end** any process (each one asks for confirmation).

The **Disk** tab answers "where did my disk space go?". It scans your home folder
(or the whole disk, another drive, or any folder you type) in seconds, then lets you
click down through folders: every folder and file is listed biggest first with its
size, its share of the folder, how many files it holds and when it last changed.
Under it, **Biggest files in here** lists the largest files anywhere inside the
folder you're looking at, with where each one lives. Common space hogs (AI model
caches, `node_modules`, Python environments, build caches, the Trash) get a short
note saying what they are and how to clear them. **Show in Finder / Explorer** opens
the item so you can decide what to delete; TaskLens never deletes files itself.

The **Alerts** tab warns you before the computer gets slow. TaskLens keeps watching
from the menu bar / tray (about 25 MB of memory, under 0.5% processor) and checks
every 15 seconds: memory about to run out (with the app that's growing and a
**Pause** button), memory or swap high, one app hogging memory, processor flat out,
disk nearly full or filling up. Alerts appear as pop-ups and can be emailed to a
list of people from your own Gmail, iCloud or Yahoo account (the app password is
kept in the system keychain). Sensitivity presets: Relaxed, Normal, Strict, or
custom numbers.

The **Report** tab covers the last 24 hours, 7 or 14 days (never more):
- every restart, with its likely reason (normal, update, crash, unexpected
  power-off or freeze) and what was happening just before
- when the computer was slow or stuck, and which app caused it
- the apps that used the most processor and memory
- the crashes, hangs and out-of-memory events the system recorded

Restarts and problems come from the system's own records (macOS `last` and
DiagnosticReports, the Windows event logs, the Linux journal). The chart comes
from a 5-minute summary TaskLens saves while running, about 100 KB a day. You can
print the report, save it as a PDF, email it, or have it emailed weekly.

Security testing, findings and the release-signing plan: [docs/SECURITY.md](docs/SECURITY.md).

**Full instructions for users: [docs/USER_GUIDE.md](docs/USER_GUIDE.md)** (also
copied into `dist/` by the build).

TaskLens runs a tiny private web server on `127.0.0.1` and opens its dashboard in
your default browser, so the same app looks and works the same on every system.
Nothing leaves the computer, and only the tab TaskLens opened can talk to it (it
uses a random per-launch key). It closes itself 3 minutes after the last TaskLens
tab is closed, or straight away from the **Quit** button.

## Installers (`dist/`)

| System | File | Install |
|---|---|---|
| macOS 11+ (Apple silicon and Intel) | `TaskLens-1.3.2-macOS.dmg` | Open it and drag **TaskLens** to **Applications**. |
| Windows 10/11 (x64) | `TaskLens-1.3.2-Windows-x64-Setup.exe` | Run it. Adds Start menu and desktop shortcuts, and an uninstaller under *Settings → Apps*. |
| Windows, no install | `TaskLens-1.3.2-Windows-x64-portable.exe`, `…-ARM64-portable.exe` | Double-click to run from anywhere (USB stick, Downloads). |
| Fedora, RHEL, CentOS, openSUSE | `tasklens-1.3.2-1.x86_64.rpm`, `….aarch64.rpm` | `sudo dnf install ./tasklens-1.3.2-1.x86_64.rpm` |
| Ubuntu, Debian, Mint | `tasklens_1.3.2-1_amd64.deb`, `…_arm64.deb` | `sudo apt install ./tasklens_1.3.2-1_amd64.deb` |

On Linux it appears in the app menu as **TaskLens**, or run `tasklens` in a terminal.
`SHA256SUMS.txt` lists checksums so people can verify their download.

### First launch warnings (the apps are not yet signed by Apple or Microsoft)

- **macOS:** "TaskLens can't be opened because Apple cannot check it". Right-click
  the app → **Open** → **Open**, once. Or *System Settings → Privacy & Security →
  Open Anyway*.
- **Windows:** "Windows protected your PC". Click **More info → Run anyway**.

These go away once the apps are signed: an Apple Developer ID ($99/year) for macOS
notarization, and a code-signing certificate for Windows.

## Managing system processes

Without admin rights you can manage your own processes. Processes owned by the system
or other users are marked **system**, and stopping them needs admin rights:

- Windows: right-click TaskLens → **Run as administrator**.
- macOS: `sudo /Applications/TaskLens.app/Contents/MacOS/TaskLens`
- Linux: `sudo tasklens`

Stopping system processes can make apps or the whole computer misbehave, so
TaskLens warns before doing it.

## Notes

- **Memory on macOS** is the same "Memory" figure Activity Monitor shows (physical
  footprint), which includes GPU and compressed memory. Most tools show only
  resident memory, which can make a 40 GB ML job look like 0.6 GB.
- **Pause** freezes a process without losing anything. On macOS that also lets the
  system reclaim GPU memory the process had locked. Its memory stays reserved until
  you resume or end it.
- **CPU %** is a share of the whole machine (100% = every core busy), like Windows
  Task Manager.
- Options: `--port N`, `--no-browser`, `--stay` (don't auto-close), `--version`.

## Building

```bash
brew install go nfpm makensis
./packaging/build.sh
```

Everything is cross-built from one Mac into `dist/`. The code is `main.go` (server
and launch), `collect.go` (process sampling and actions), `disk.go` (disk scanner), `alerts.go` (alert rules), `notify.go` (pop-ups and email), `settings.go`, `tray.go`, `autostart_*.go`, `web/index.html`
(dashboard), and `packaging/` (icon, installer definitions). Change `version` in
`main.go` to release a new version.
