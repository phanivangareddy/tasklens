# TaskLens security review and test plan

Reviewed 12 Nov 2026 against TaskLens 1.3.0 on macOS 26.5.1. All the app findings
(F1–F10) are fixed in **1.3.1** and were tested again on the rebuilt app. Every result
below comes from a test run on this Mac unless it says **not tested**.

## 1. What an attacker could go after

TaskLens can see every running app, pause or stop apps, open Finder, read crash
reports, and holds an email app password. So the main questions are:

1. Can **a website** you visit control TaskLens through its local web page?
2. Can **another program or another person on the computer** control it or read its data?
3. Can a **booby-trapped name** (an app, folder, file or crash report) run code in the dashboard, the pop-ups or the emails?
4. Can someone **read the email password**?
5. Can someone **give people a fake TaskLens** (same name, changed code) and have it trusted?

## 2. Tests run and results

| # | Attack | Result |
|---|---|---|
| 1 | Call the API with no key | **Blocked** (403) |
| 2 | Call the API with a wrong key | **Blocked** (403) |
| 3 | Put the key in the web address instead of the header | **Blocked** (403) |
| 4 | DNS rebinding: a website points its own name at 127.0.0.1 | **Blocked** (Host check, 403) |
| 5 | Another website sends a cross-site request (CORS preflight) | **Blocked** (403, no CORS allowed) |
| 6 | A hidden form on another website posts "end process" | **Blocked** (403) |
| 7 | Reach TaskLens from another computer on the Wi-Fi | **Refused**: it only listens on 127.0.0.1 |
| 8 | Stop process 1 (launchd) even with the key | **Refused** |
| 9 | "Show in Finder" on /etc/passwd, or `/Users/you/../../etc` | **Refused** (outside the scanned area) |
| 10 | Read the key file as another user | **Protected**: file is `-rw-------`, owner only |
| 11 | App named `<img src=x onerror=…>`: Processes list, health notes, End dialog | **Safe**: shown as text, never run |
| 12 | Folder and file with that name in the Disk tab, breadcrumbs, Biggest files | **Safe** |
| 13 | Crash report with that app name in the Report tab | **Safe** |
| 14 | Pop-up text with quotes and code | **Safe**: the pop-up library passes text as JSON, not as script |
| 15 | Known vulnerabilities (govulncheck v1.1.4) | 2 found in 1.3.0 (F5); **none** in 1.3.1 |
| 16 | App name containing a line break, sent in an alert email | Was a problem (F1), **fixed in 1.3.1** |
| 17 | A special "pipe" file dropped in the crash-report folder | Was a problem (F2), **fixed in 1.3.1** |
| 18 | Another program reads the saved email password | Was a problem (F3), **fixed in 1.3.1** on Mac |
| 19 | Changing the mail server, then sending | **Password forgotten** (F4, 1.3.1) |
| 20 | A 200 KB request | **Rejected** (F8, 1.3.1) |

## 3. Findings and fixes

| ID | Severity | What was wrong | Fixed in 1.3.1 | Checked again |
|---|---|---|---|---|
| F1 | Medium | **Email header injection.** An app's name went into the email subject unchanged, so a program with a line break in its name could add its own headers or text to alert emails, for example a fake "call this number" message to your family. | Every name from outside (apps, command lines, files, crash and event-log records) has line breaks and control characters replaced before it is shown or sent. The email headers are cleaned again just before sending. | Test: the crafted subject stays on one line. |
| F2 | Low | **Report could freeze.** A special pipe file in `~/Library/Logs/DiagnosticReports` made TaskLens wait forever. | Only ordinary files are read. Pipes, devices and links are skipped. | Test: a pipe and a link in the folder are skipped, and reading finishes. |
| F3 | Medium | **Email password readable without a prompt.** The password was saved through Apple's `security` tool, so any program running as you could read it silently. | On Mac, TaskLens now saves it with the Keychain API itself, so the Keychain trusts only TaskLens. Passwords saved by older versions are moved over on first use. | Live: TaskLens reads it; `security find-generic-password -w` gets a macOS permission prompt, not the password. |
| F4 | Low | **Changing the mail server kept the password**, so a changed setting could send it to another server. | Changing the server, port or sender address deletes the saved password, and it must be typed in again. | Test, plus live: after pointing email at another server, the Keychain item was gone. |
| F5 | Low | Go 1.27.3 net/http (GO-2026-6859) and golang.org/x/image v0.44.0 (GO-2026-6222). | Builds with Go 1.27.4 (`toolchain go1.27.4` in go.mod) and x/image v0.45.0. | govulncheck: **No vulnerabilities found.** |
| F6 | Low | No browser security headers. | Every response sends Content-Security-Policy (scripts only from TaskLens itself, so the dashboard script moved to `web/app.js`), X-Frame-Options DENY, nosniff, Referrer-Policy no-referrer and same-origin isolation. The page itself also refuses other host names. | Headers present; dashboard works with no CSP errors. |
| F7 | Low | The dashboard key stayed in the address bar and browser history. | The page keeps the key in this tab only (sessionStorage) and removes it from the address straight away. Reloading still works. | Address shows `http://127.0.0.1:PORT/` with no key; reload kept working. |
| F8 | Info | Request size not limited. | API requests are limited to 64 KB. Read, idle and header timeouts and a header size limit are set. The key is compared in constant time. | A 200 KB request is rejected. |
| F9 | Info | Windows "Show in Explorer" with a comma in a folder name. | For names with a comma or quote, Explorer opens the folder that holds the item instead of using `/select,`. | **Not tested** on Windows. |
| F10 | Info | System tools looked up on the PATH. | `open`, `launchctl`, `journalctl`, `coredumpctl`, `xdg-open`, `wevtutil`, `explorer` and `rundll32` are run from their system folders. | Builds and runs on Mac; **not tested** on Windows or Linux. |

**Also fixed while retesting:** macOS deleted its own 5 Nov crash report this
afternoon, so the Report briefly showed that restart as "unexpected". TaskLens now
reads the summary file macOS keeps (`.contents.panic`), and it remembers every
restart reason it has found for 14 days, so reasons survive the system's cleanup.

**Still true on Windows and Linux:** Credential Manager and the Linux keyring let
any program running as the same user read saved passwords; that is how those
systems are designed. On those systems, send alerts from a **separate email
account used only for TaskLens**.

**After updating TaskLens on a Mac,** macOS may ask once whether TaskLens can use
its saved password. Click **Always Allow**. This stops once the app is signed with
a Developer ID (R1), because macOS then recognises each new version as the same app.

## 4. Fake copies: someone changes the code and publishes it as "TaskLens"

This is the biggest risk once TaskLens is public. Today:

| Risk | Today | Why it matters |
|---|---|---|
| R1 | The Mac app is only **ad-hoc signed** and fails Gatekeeper (`spctl`: rejected). The Windows exe is **unsigned**. | A genuine copy and a fake copy both show the same "unidentified developer" warning, so users can't tell them apart. |
| R2 | `SHA256SUMS.txt` sits next to the files and isn't signed. | Anyone who can replace the files can replace the checksums too. |
| R3 | The code isn't in git yet, so there's no history, review or protected branch. | Once published, a stolen account or a bad merge could ship changed code. |
| R4 | The name **TaskLens is already used** by other products: a staff tracker on Google Play ([listing](https://play.google.com/store/apps/details?id=com.tasklens)), an AI productivity app on the App Store ([listing](https://chrome-stats.com/d/id6762246508)) and an Obsidian plugin ([listing](https://dev.obsidianstats.com/plugins/tasklens)). | Search results will mix products, a lookalike download is easy to pass off, and there may be a trademark conflict when you sell. |
| R5 | Linux .deb/.rpm packages aren't GPG-signed. | Package managers can't check who built them. |
| R6 | No automatic updates yet. | Good for now: there is no update channel to hijack. When updates are added, every update must be signature-checked. |

**What stops fake copies**

1. **Sign everything with keys only you hold.**
   - Mac: an Apple Developer ID (about $99 a year), hardened runtime, and notarization. A changed app then fails to open, and a fake can't be signed with your name.
   - Windows: Authenticode signing (for example Azure Trusted Signing, about $10 a month), so the publisher shows as your name.
   - Linux: sign the .deb/.rpm with a GPG key and publish the public key on your website.
2. **One official download place.** Your own domain over HTTPS, plus GitHub Releases. Say clearly on the site and in the guide that TaskLens is only downloaded from there.
3. **Signed checksums.** Sign `SHA256SUMS.txt` with minisign or cosign, and print the public key on the website and in the guide.
4. **Build in the open.** Build releases on GitHub Actions from a tagged commit, with build provenance attestations, so anyone can check which source a file came from.
5. **Lock down the code.** Use 2-factor sign-in, a protected `main` branch, signed commits, no direct pushes, and review before merge. Keep the signing keys out of the repository, in GitHub's encrypted secrets or a hardware key.
6. **Claim the name everywhere.** Pick a distinct name before selling (R4), buy the domain, and reserve it on Homebrew, winget, Snap/Flathub and GitHub so nobody else can publish under it.
7. **Stay alert.** Search now and then for copies, and have a security contact (`security@yourdomain`) so people can report fakes.

## 5. How to test TaskLens yourself

Run these before every release. Claude Code can also walk through them using the
14 security skills in [`.claude/skills`](../.claude/skills/README.md). In a Claude
session opened in the `tasklens` folder, ask for example "run the release security
checks on TaskLens".

**Automatic checks (on the build machine)**

```bash
go vet ./... && go test ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...
go run github.com/securego/gosec/v2/cmd/gosec@v2.21.4 -exclude-dir tools ./...
```

`gosec` couldn't be run during this review (the automatic permission check blocked it). Run it once yourself and review what it reports.

**Attack the running app** (TaskLens open; the key is in `~/Library/Application Support/TaskLens/instance.json`):

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:PORT/api/snapshot                     # expect 403
curl -s -o /dev/null -w '%{http_code}\n' -H 'X-Token: KEY' -H 'Host: evil.example' http://127.0.0.1:PORT/api/snapshot   # expect 403
curl -s -H 'X-Token: KEY' -d '{"pid":1,"action":"force"}' http://127.0.0.1:PORT/api/action       # expect refusal
curl -s -H 'X-Token: KEY' -d '{"path":"/etc/passwd"}' http://127.0.0.1:PORT/api/disk/reveal      # expect refusal
```

**Booby-trapped names:** make a folder, a file and a small program named
`<img src=x onerror=alert(1)>`, open each TaskLens tab, and check that no alert box
appears and the name shows as plain text.

**Each system:** repeat the checks on Windows 11 and Ubuntu, where they have **not been run yet**.

**Release files:** on a clean computer, check that the Mac app opens without a
warning (`spctl -a -vv TaskLens.app` says "accepted"), the Windows installer shows
your name as publisher, and the checksums and signatures verify.

**Outside review:** before charging money, have someone independent review it,
or run a small bug bounty.
