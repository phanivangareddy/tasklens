# TaskLens User Guide

**Version 1.3.1 · For Windows, macOS and Linux · Home use**

TaskLens watches your computer and tells you, in plain words, what is slowing it
down. It also shows what is filling your disk, and it **warns you before** memory or
disk space run out, with a pop-up on the computer and, if you like, an email to you
and your family. Its **Report** shows the last two weeks: every restart and why it
happened, when the computer was slow or stuck and which app caused it, and which
apps used the most.

---

## Contents

1. [What you need](#1-what-you-need)
2. [How much TaskLens itself uses](#2-how-much-tasklens-itself-uses)
3. [Installing](#3-installing)
4. [A quick tour](#4-a-quick-tour)
5. [Alerts and pop-ups](#5-alerts-and-pop-ups)
6. [Email alerts for you and your family](#6-email-alerts-for-you-and-your-family)
7. [Running in the background](#7-running-in-the-background)
8. [Privacy and safety](#8-privacy-and-safety)
9. [Uninstalling](#9-uninstalling)
10. [Troubleshooting](#10-troubleshooting)
11. [Advanced options](#11-advanced-options)

---

## 1. What you need

| | Windows | macOS | Linux |
|---|---|---|---|
| Version | Windows 10 or 11 | macOS 11 Big Sur or newer | Any recent desktop (Ubuntu 22.04+, Fedora 38+, Debian 12+, Mint 21+) |
| Processor | Intel/AMD 64-bit or ARM64 | Apple silicon or Intel | x86-64 or ARM64 |
| Disk space | 10 MB | 10 MB | 10 MB |
| Browser | Any (Edge, Chrome, Firefox) | Any (Safari, Chrome, Firefox) | Any |

The dashboard opens in your normal web browser, but it is **not a website**: it
runs entirely on your own computer and works with no internet connection. The
internet is only needed to send email alerts.

## 2. How much TaskLens itself uses

A monitor shouldn't slow the computer down. Measured on a Mac with 48 GB of memory:

| What TaskLens is doing | Memory (RAM) | Processor |
|---|---|---|
| Running in the background (menu bar / tray only) | **about 22–25 MB** | **under 0.5%** (checks every 15 seconds) |
| Dashboard open | about 25–40 MB | about 1–2% (refreshes every 2 seconds) |
| Right after a full disk scan | about 40 MB | a burst of a few seconds while scanning |
| Quit | 0 | 0 |

For comparison, one web browser tab usually uses 100–300 MB.

## 3. Installing

Download the file for your computer:

| Your computer | Download |
|---|---|
| Mac (any) | `TaskLens-1.3.1-macOS.dmg` |
| Windows (most PCs) | `TaskLens-1.3.1-Windows-x64-Setup.exe` |
| Windows, no install (USB stick, work PC) | `TaskLens-1.3.1-Windows-x64-portable.exe` |
| Windows on ARM (Surface Pro X, Copilot+ PCs) | `TaskLens-1.3.1-Windows-ARM64-portable.exe` |
| Fedora, RHEL, CentOS, openSUSE | `tasklens-1.3.1-1.x86_64.rpm` (or `.aarch64.rpm`) |
| Ubuntu, Debian, Mint, Pop!_OS | `tasklens_1.3.1-1_amd64.deb` (or `_arm64.deb`) |

> **Checking the download (optional):** `SHA256SUMS.txt` lists a fingerprint for
> every file. On Mac/Linux run `shasum -a 256 <file>`; on Windows run
> `certutil -hashfile <file> SHA256` in Command Prompt, and compare.

### 3.1 macOS

1. Double-click **TaskLens-1.3.1-macOS.dmg**. A window opens showing TaskLens and
   your Applications folder.
2. **Drag the TaskLens icon onto the Applications folder.**
3. Eject the disk image (click ⏏ next to "TaskLens" in Finder's sidebar).
4. Open **Applications** and **right-click TaskLens → Open**, then click **Open**
   again. *(Only needed the first time. macOS shows "Apple cannot check it for
   malicious software" for apps from outside the App Store; right-click → Open
   tells it you trust this one. If you double-clicked and got stuck, go to
   **System Settings → Privacy & Security**, scroll down and click **Open Anyway**.)*
5. Your browser opens the TaskLens dashboard, and a small **pulse icon** appears in
   the menu bar at the top right of the screen.

**Optional, for complete disk scans:** some folders (Mail, Messages, Safari, other
users' folders) are protected by macOS. To include them, open **System Settings →
Privacy & Security → Full Disk Access**, click **+**, choose **TaskLens** in
Applications, and switch it on. Without this, TaskLens still works; it just tells
you how many folders it couldn't read.

### 3.2 Windows

**Installer (recommended)**

1. Double-click **TaskLens-1.3.1-Windows-x64-Setup.exe**.
2. If Windows shows **"Windows protected your PC"**, click **More info → Run
   anyway**. *(This appears for new apps that aren't yet widely downloaded.)*
3. Click **Yes** when asked whether to allow changes (it installs into Program
   Files), then **Install**, then **Finish**.
4. TaskLens opens in your browser, and its icon appears in the **notification area**
   by the clock. If you don't see it, click the **^** arrow by the clock; you can
   drag the icon out so it's always visible.

A Start menu entry and a desktop shortcut are created.

**Portable (no install)**

Double-click **TaskLens-1.3.1-Windows-x64-portable.exe** from anywhere. Nothing is
installed; settings are kept in your user profile.

### 3.3 Linux

Open a terminal in the folder where you downloaded the file.

**Ubuntu, Debian, Mint, Pop!_OS**

```bash
sudo apt install ./tasklens_1.3.1-1_amd64.deb
```

**Fedora, RHEL, CentOS Stream**

```bash
sudo dnf install ./tasklens-1.3.1-1.x86_64.rpm
```

**openSUSE**

```bash
sudo zypper install ./tasklens-1.3.1-1.x86_64.rpm
```

Then open **TaskLens** from your app menu, or type `tasklens` in a terminal.

Notes for Linux:

- **Pop-ups** use the standard `zenity` and `notify-send` tools. The package asks for
  them, and most desktops already have them. If pop-ups don't appear, install them:
  `sudo apt install zenity libnotify-bin` or `sudo dnf install zenity libnotify`.
- **Tray icon:** KDE, Xfce, Cinnamon, MATE and Ubuntu show it out of the box. On
  plain GNOME (e.g. Fedora), install the **AppIndicator and KStatusNotifierItem
  Support** extension to see it. TaskLens works without the icon, too.

## 4. A quick tour

The dashboard has three tabs at the top: **Processes**, **Disk** and **Alerts**.
Across the top are live cards for **CPU** (processor), **Memory**, **Swap** (memory
the computer has had to move onto the disk, which is slow), **Disk** free space, and
how long the computer has been **up** since it last restarted.

### 4.1 Processes: what's running

- **What's slowing things down** explains the situation in a sentence or two, for
  example: *"python3.11 is using 48 GB (more than all of your memory, so the rest
  has been pushed to disk) and has been running for 2m 45s."* Click an app's name to
  jump to it in the list.
- The list shows every running program: its **CPU** and **Memory** use, how long it
  has been **running**, how much **CPU time** it has used in total, and who it
  belongs to. Click a column heading to sort by it. Type in the search box to find
  something.
- **Filters:** *All*, *My processes* (just yours), *Heavy* (only the ones using a
  lot), *Paused*.
- **Actions** on every row (each asks you to confirm first):
  - **Pause** freezes the app in place. Nothing is lost, and it stops using the
    processor. Use **Resume** to carry on. Great for a big job you want to finish
    later.
  - **End** asks the app to close, so it can save its work.
  - **Force** stops it immediately. Anything unsaved is lost; use it only if End
    doesn't work.
- Apps marked **system** belong to the operating system or another user. TaskLens
  warns you before touching them, and needs to run as administrator to do so (see
  [Advanced options](#11-advanced-options)).
- **Freeze** stops the list from updating so you can read it.

### 4.2 Disk: where your space went

1. Click **Disk** (or the Disk card). TaskLens scans your **Home** folder; this
   takes a few seconds to a minute or two.
2. You'll see each folder and file **biggest first**, with its size, a bar showing
   its share, how many files it holds and when it last changed.
3. **Click any folder** to look inside it. The path at the top (e.g.
   *Home › .cache › huggingface › hub*) lets you go back up.
4. **Biggest files in here** lists the largest files anywhere inside the folder
   you're looking at, with where each one lives. This finds the one 40 GB video
   hiding five folders deep.
5. Folders that are often big get a short note saying what they are and how to
   clear them safely, for example *"node_modules: JavaScript packages for one
   project. Safe to delete; npm install brings them back."*
6. **Show in Finder / Show in Explorer** opens the item so you can decide what to
   do. **TaskLens never deletes anything itself.**

Use **Whole disk** to scan everything, or type any folder or drive in the box and
click **Scan**.

### 4.3 Alerts

See the next two sections.

### 4.4 Report: the last two weeks

The **Report** tab answers "what happened to my computer?" for the last **24 hours,
7 days or 14 days**. It never keeps more than 14 days; older history is deleted
automatically.

- **In short**: a few plain sentences, for example *"Restarted 6 times in the last
  14 days (1 crashed, 3 turned off unexpectedly, 2 normal)"* or *"Slowest stretch:
  memory was over 90% full for 4 hours from Wed 11 Nov 21:00 (python3.11 was using
  35 GB)."*
- **Processor and memory over time**: a chart of both, with slow periods shaded
  red, times the computer was off or asleep shaded grey, and a red line at each
  restart. Point at the chart to see the numbers at that moment.
- **Restarts**: when the computer started again and the most likely reason:

  | Label | What it means |
  |---|---|
  | **Normal** | Someone chose Restart or Shut Down, or an app or installer did. |
  | **Update** | It restarted to install a system update (TaskLens also spots the version changing). |
  | **Crash** | The system itself crashed (a Mac "kernel panic" or a Windows blue screen) and restarted. If the Mac froze first, it says for how long. |
  | **Unexpected** | It went off without shutting down: the power was cut, the power button was held down, or it froze. If memory was full just before, the report says so. |
  | **Unknown** | The system kept no record TaskLens could read. |

  When TaskLens was running beforehand, it also shows **what was happening in the
  30 minutes before**: how full memory was, how busy the processor was, and the
  biggest apps. It also notes when **shutting down was slow** because an app
  took a long time to close.
- **When it was slow or stuck**: every time memory was over 90% full, or the
  processor over 90% busy, for 10 minutes or more, how long it lasted, and the
  app most responsible.
- **Apps that used the most**: each app's share of all processor use, its total
  processor time, and the most memory it ever used (and when). Helpers with the
  same name, like browser tabs, are added together.
- **Problems the system recorded**: app crashes, apps that stopped responding,
  times memory ran out and the system closed apps to keep going, and apps that
  wrote huge amounts to disk. Repeats within an hour are grouped ("5 times, until
  07:09").

**Sharing the report**

- **Print or save as PDF** opens the print window; choose *Save as PDF* to get a
  file you can send to anyone.
- **Email to my list** sends a plain-text version to everyone on your alert list
  (set up email first, see section 6).
- Tick **Email this report to my list every week** at the bottom of the Report tab
  to get a 7-day summary each week, sent in the daytime.

**Where the information comes from**

- **Restarts and problems** come from the records the system already keeps, so they
  cover the whole two weeks even if you only just installed TaskLens:
  - Mac: the login records (`last`) and the reports in `/Library/Logs/DiagnosticReports`
  - Windows: the System and Application event logs
  - Linux: the systemd journal and `coredumpctl`. On some Linux systems you need to
    be in the `adm` or `systemd-journal` group to read it; otherwise restarts show
    as "Unknown".
- **The chart, slow times and app usage** come from TaskLens's own history. It saves
  a small summary every 5 minutes (about 100 KB a day, under 2 MB for two weeks), but
  only while TaskLens is running. Turn on **Start at login** (section 7) so there are
  no gaps. You can switch the history off with **Keep a 2-week history** at the
  bottom of the Report tab.

## 5. Alerts and pop-ups

TaskLens checks the computer every **15 seconds** and warns you when something
needs attention, ideally **before** you notice a slowdown.

### 5.1 What the pop-ups look like

- **Urgent alerts** (memory about to run out, or full) open a small window:

  > **Memory will run out in about 4 minutes**
  > python3.11 has grown by 12 GB in the last few minutes and now uses 44 GB.
  > Pause or close it before the computer slows down.
  >
  > [ Dismiss ] [ Pause python3.11 ] [ Open TaskLens ]

  **Pause** freezes the app straight away, so you can resume it later. The window
  closes by itself after 3 minutes if you ignore it. (The Pause button only appears
  for your own apps, never for system ones.)
- **Warnings** (high processor use, a lot of swap, disk getting full) appear as
  normal notifications in the corner of the screen.

To see what they look like, open **Alerts** and click **Show a test notification**
or **Show a test urgent pop-up**.

### 5.2 How sensitive should it be?

Pick **Relaxed**, **Normal** (the default) or **Strict**, or change any number
yourself (it then shows **Custom**).

| Alert | What it means | Relaxed | Normal | Strict |
|---|---|---|---|---|
| **Early warning** *(urgent)* | Memory is filling so fast it will run out within… | 3 min | 5 min | 10 min |
| **Memory full** *(urgent)* | Memory in use stays above… for a minute | 95% | 90% | 85% |
| **One big app** | A single app uses more than… of memory, for a minute | 75% | 50% | 35% |
| **Swap** | Memory pushed onto the disk is more than… of your RAM | 50% | 25% | 10% |
| **Processor busy** | The processor stays above… for… | 95% / 10 min | 90% / 5 min | 80% / 3 min |
| **Disk almost full** | Free space drops below… | 5% | 10% | 15% |
| **Disk filling up** | At the current rate, the disk will be full within… | 7 days | 14 days | 30 days |
| **Quiet period** | The same alert isn't repeated within… | 60 min | 30 min | 15 min |

Untick any alert you don't want. Short spikes are ignored: most alerts only fire if
the problem lasts at least a minute. The **disk filling up** forecast needs about
half a day of watching before it can predict anything.

### 5.3 Taking a break from alerts

From the menu bar / tray icon choose **Pause alerts for 1 hour** (for example
while you render a video on purpose). Choose **Resume alerts** to turn them back on
early. To stop pop-ups completely, switch off **Pop-ups on this computer** in the
Alerts tab.

## 6. Email alerts for you and your family

TaskLens can email alerts to you, a partner, or whoever looks after the computer.
Emails are sent **from your own email account**; nothing goes through TaskLens
servers.

### 6.1 Step 1: get an "app password" for the sending account

Email providers don't let apps use your normal password. Instead you create a
special **app password** just for TaskLens. It only takes a minute, and you can
cancel it at any time without changing your real password.

**Gmail**

1. Make sure **2-Step Verification** is on for your Google account
   (myaccount.google.com → Security).
2. Go to **myaccount.google.com/apppasswords**.
3. Type **TaskLens** as the app name and click **Create**.
4. Google shows a 16-letter password like `abcd efgh ijkl mnop`. Copy it (the
   spaces don't matter).

**iCloud Mail**

1. Sign in at **account.apple.com** → **Sign-In and Security** → **App-Specific
   Passwords**.
2. Click **+**, name it **TaskLens**, and copy the password.
3. Send from your **@icloud.com** (or @me.com) address.

**Yahoo Mail**

1. Open **Account Info → Account Security → Generate app password**.
2. Name it **TaskLens** and copy the password.

**Other providers** (Fastmail, Zoho, your internet provider…): choose **Other**,
then enter the provider's **outgoing (SMTP) server** and **port** from their help
pages. Port 587 or 465 both work. TaskLens always uses an encrypted connection and
refuses to send your password over an unencrypted one.

> **Outlook.com / Hotmail:** Microsoft stopped allowing app passwords for personal
> Outlook accounts in September 2024, so they can't send TaskLens alerts. Use a
> Gmail, iCloud or Yahoo account to *send*. Outlook addresses can still *receive*
> alerts.

### 6.2 Step 2: set it up in TaskLens

1. Open TaskLens → **Alerts** → **Email alerts**.
2. **Send from:** choose Gmail, iCloud Mail, Yahoo Mail or Other.
3. **Sender email address:** the account you made the app password for.
4. **App password:** paste it and click **Save password**. You'll see **✓ Saved in
   Keychain** (Mac), **Credential Manager** (Windows) or **keyring** (Linux): the
   password is kept in your system's secure password store, not in a plain file.
5. **People to alert:** type an email address and click **Add person**. Repeat for
   everyone who should be told. Anyone can receive alerts at any email address,
   including Outlook and work addresses.
   - Tick **Urgent alerts only** for people who should only hear about serious
     problems (memory about to run out or full, disk almost completely full).
   - Click **Remove** to take someone off the list.
6. Click **Send a test email**. Everyone on the list should get it within a minute;
   ask them to check their **spam** folder the first time and mark it "Not spam".
7. Switch **Email alerts** **On** (top right of the section).

### 6.3 What an alert email looks like

> **Subject:** [TaskLens] Memory will run out in about 4 minutes on My-MacBook-Pro
>
> Urgent: Memory will run out in about 4 minutes
>
> python3.11 has grown by 12 GB in the last few minutes and now uses 44 GB.
> Pause or close it before the computer slows down.
>
> Computer: My-MacBook-Pro (macOS 26.5.1)
> Time: Tue 11 Nov 2026, 14:02
>
> Right now: processor 12% busy · memory 93% (45 GB of 48 GB) · swap 17 GB · disk 535 GB free
> Biggest apps:
> • python3.11: 44 GB memory, 3% processor
> • qemu-system-aarch64: 6.1 GB memory, 1% processor
> • …
>
> What to do: open TaskLens on that computer to pause or close the app.

TaskLens never sends more than **10 emails an hour**, and repeats of the same alert
wait for the quiet period, so nobody's inbox gets flooded.

### 6.4 If the test email fails

| Message | What to do |
|---|---|
| *"the email provider refused the sign-in"* | Use the **app password**, not your normal password. Check the sender address is exactly the account the app password belongs to. For Gmail, check 2-Step Verification is on. |
| *"the app password … hasn't been saved yet"* | Paste it again and click **Save password**. |
| *"couldn't reach smtp…"* | Check the internet connection. Some public Wi-Fi and work networks block email ports 587/465; try at home. |
| *"… was refused"* | That recipient address is mistyped or doesn't exist. |
| *"doesn't offer an encrypted connection"* | Your provider's server/port is wrong; check their SMTP settings. |
| Nobody received it | Check spam folders. Gmail may take a minute the first time. |

To **stop** emails, switch **Email alerts** off, or remove people from the list.
To cancel TaskLens's access completely, delete the app password in your email
account's security settings.

## 7. Running in the background

With **Keep watching after I close this page** switched on (it is by default),
closing the dashboard doesn't stop TaskLens: it keeps watching from the **menu bar**
(Mac) or **notification area / tray** (Windows, Linux), using about 25 MB of memory.

Click the icon for:

- **Status line:** current processor, memory and free disk space at a glance
- **Open TaskLens:** opens the dashboard (on Windows and Linux, clicking the icon
  itself does this too)
- **Pause alerts for 1 hour** / **Resume alerts**
- **Start at login:** tick to start TaskLens quietly every time you log in, so
  you're always covered
- **Quit TaskLens:** stops watching completely

You can also switch **Start at login** on in **Alerts → Running in the background**.
On a Mac, TaskLens must be in your **Applications** folder for this (not run
straight from the disk image).

If you switch background watching off, TaskLens closes itself 3 minutes after you
close the last dashboard tab, and alerts stop until you open it again.

Opening TaskLens again while it's already running just opens the dashboard; it never
starts twice.

## 8. Privacy and safety

- **Everything stays on your computer.** TaskLens has no account and no tracking,
  and doesn't send your data anywhere. The only outgoing connections are the alert
  emails you set up, straight from your computer to your own email provider.
- The dashboard is only reachable from **your own computer** (127.0.0.1), and only
  by the browser tab TaskLens opened: each launch uses a new random key, so other
  websites can't control it.
- Your email password is kept in the system's secure password store. On a Mac it
  goes in the Keychain, where only TaskLens can read it: any other program gets a
  macOS prompt instead. On Windows (Credential Manager) and Linux (the keyring),
  other programs running as you can read saved passwords, so **use a separate email
  account just for TaskLens alerts**. If a Linux computer has no keyring, TaskLens
  says so and stores the password in its own settings file, readable only by you.
- Changing the mail server or sender address deletes the saved password, so you
  type it in again. That way a changed setting can't send it somewhere else.
- After an update on a Mac, macOS may ask once whether TaskLens can use its saved
  password. Click **Always Allow**.
- The dashboard key is removed from the browser's address bar as soon as the page
  opens, so it doesn't end up in your browser history.
- Only download TaskLens from its official page, and see
  [SECURITY.md](SECURITY.md) for how it was tested.
- TaskLens **never deletes files** and **never stops anything without asking you**.
  The only automatic action is the **Pause** button you press in an alert.
- The two-week history is a small summary every 5 minutes: overall processor and
  memory use and the names of the busiest apps. No file names, window titles,
  websites or typing are recorded. Days older than 14 are deleted automatically.
- Settings and history are kept in:
  - Mac: `~/Library/Application Support/TaskLens/`
  - Windows: `%APPDATA%\TaskLens\`
  - Linux: `~/.config/TaskLens/`

## 9. Uninstalling

First choose **Quit TaskLens** from the menu bar / tray icon.

**macOS**

1. Drag **TaskLens** from Applications to the Trash.
2. If you used Start at login, also delete
   `~/Library/LaunchAgents/app.tasklens.agent.plist` (in Finder: **Go → Go to
   Folder…**, paste `~/Library/LaunchAgents`). Turning **Start at login** off before
   uninstalling does this for you.
3. Optional: delete `~/Library/Application Support/TaskLens`, and in **Keychain
   Access** search for "TaskLens email" and delete it.

**Windows**

1. **Settings → Apps → Installed apps → TaskLens → Uninstall.** (Portable version:
   just delete the .exe.)
2. If you used Start at login, turn it off first, or remove TaskLens from **Task
   Manager → Startup apps**.
3. Optional: delete `%APPDATA%\TaskLens`, and in **Credential Manager → Windows
   Credentials** remove "TaskLens email".

**Linux**

```bash
sudo apt remove tasklens        # Ubuntu, Debian, Mint
sudo dnf remove tasklens        # Fedora, RHEL
rm -f ~/.config/autostart/tasklens.desktop
rm -rf ~/.config/TaskLens       # optional: settings
```

## 10. Troubleshooting

| Problem | Fix |
|---|---|
| **Mac: "TaskLens can't be opened"** | Right-click the app → **Open** → **Open**. Or System Settings → Privacy & Security → **Open Anyway**. |
| **Windows: "Windows protected your PC"** | Click **More info → Run anyway**. |
| **The dashboard didn't open** | Click the TaskLens icon → **Open TaskLens**, or open TaskLens again from your apps. |
| **I can't see the icon** | Mac: the menu bar may be full; quit some menu bar apps, or hold ⌘ and drag icons to rearrange. Windows: click **^** by the clock. GNOME: install the AppIndicator extension (see 3.3). |
| **No pop-ups appear** | Check **Pop-ups on this computer** is on, and alerts aren't paused. Mac: System Settings → **Notifications** → allow notifications for **Script Editor** (macOS shows TaskLens's notifications under that name). Windows: Settings → System → Notifications → on, and Focus/Do Not Disturb off. Linux: install `zenity` and `libnotify`. |
| **"Permission denied" when ending an app** | It belongs to the system or another user; see [Advanced options](#11-advanced-options) to run as administrator. |
| **Disk scan says some folders couldn't be read** | Mac: give TaskLens Full Disk Access (3.1). Windows: run as administrator. Linux: run with `sudo`. |
| **"Start at login" gives an error on Mac** | Move TaskLens into Applications, open it from there, then switch it on again. |
| **Alerts come too often / not at all** | Change sensitivity (5.2) or the quiet period. |
| **The page says "Disconnected"** | TaskLens was quit. Open it again. |
| **The Report chart is mostly grey** | TaskLens only records while it runs. Turn on **Start at login**. The restart list still covers the full two weeks. |
| **Linux: every restart says "Unknown"** | Add yourself to the journal readers, then log out and back in: `sudo usermod -aG systemd-journal $USER` |

## 11. Advanced options

**Running as administrator**, to manage system processes and scan every folder:

- Windows: right-click TaskLens → **Run as administrator**.
- Mac: `sudo /Applications/TaskLens.app/Contents/MacOS/TaskLens`
- Linux: `sudo tasklens`

Stopping system processes can make apps or the whole computer misbehave, so
TaskLens always warns first.

**Command-line options** (`tasklens --help`):

| Option | What it does |
|---|---|
| `--background` | Start quietly in the menu bar / tray without opening the dashboard (what Start at login uses) |
| `--no-browser` | Don't open the dashboard automatically |
| `--no-tray` | Don't show the menu bar / tray icon |
| `--stay` | Keep running even if background watching is switched off |
| `--port N` | Use a fixed port for the dashboard |
| `--version` | Show the version |
