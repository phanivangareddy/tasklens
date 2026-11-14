# Security skills for TaskLens

These 14 skills are copied unchanged from the community project
[mukul975/Anthropic-Cybersecurity-Skills](https://github.com/mukul975/Anthropic-Cybersecurity-Skills),
which is **not made by Anthropic**. They come from commit
`54a798831d2266a3ca61ce68a7acb80b81160d57` (31 Aug 2026) and are licensed under
Apache 2.0 (see `LICENSE-cybersecurity-skills` and the `LICENSE` in each folder).

Claude Code loads them automatically when working in this project. Each folder has
a `SKILL.md` (the steps), `references/` (background) and `scripts/` (optional helpers).

## Which skill for what

| When | Skill | What it checks in TaskLens |
|---|---|---|
| Every release | `testing-api-security-with-owasp-top-10` | The dashboard API against the OWASP API Top 10 |
| Every release | `testing-for-host-header-injection` | Fake and duplicate Host headers (DNS rebinding) |
| Every release | `testing-cors-misconfiguration` | That no other website is allowed to call the API |
| Every release | `performing-csrf-attack-simulation` | That another site can't make the browser send commands |
| Every release | `testing-for-xss-vulnerabilities` | That crafted app, file and folder names can't run code |
| Every release | `testing-for-email-header-injection` | That alert emails can't be altered through names or addresses |
| Every release | `implementing-secret-scanning-with-gitleaks` | No keys or passwords in the code or its history |
| Every release | `generating-and-analyzing-sboms` | The list of libraries shipped, and known flaws in them |
| Ongoing | `implementing-semgrep-for-custom-sast-rules` | Code-scanning rules for TaskLens's own risky patterns |
| Before publishing | `implementing-code-signing-for-artifacts` | Signing the installers so fakes can be told apart |
| Before publishing | `implementing-sigstore-for-software-signing` | Signing release files and checksums with Sigstore |
| Before publishing | `verifying-build-provenance-with-slsa-sigstore` | Proof of which source code built each file |
| Before publishing | `securing-github-actions-workflows` | A safe build pipeline once the code is on GitHub |
| Before publishing | `detecting-dependency-confusion` | That no look-alike package can sneak into the build |

## Notes from the review (12 Nov 2026)

- Nothing in these folders runs on its own. The scripts only run when you or
  Claude start them, and they only contact the address you give them plus public
  registries (PyPI, npm, the Sigstore transparency log).
- Point the testing scripts only at your own copy of TaskLens, such as
  `http://127.0.0.1:PORT`.
- The SBOM skill installs Syft and Grype with `curl … | sh`. Use
  `brew install syft grype` instead.
- Sigstore "keyless" signing writes the signer's email address into a public log
  that can never be erased. Use a project email address for releases, not a
  personal one.
- To update: review the new version first, then copy over the folders and update
  the commit above.
