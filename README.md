# CV++ — increment your resume

CV++ is a local-first resume workspace for the IIT Kharagpur ERP portal. Download the latest release, double-click the application, and follow the browser setup wizard. You do not need Git, Go, a terminal, or knowledge of JSON.

## Private by design

Your ERP roll number, password, security answers, session, resume content, recovery backups, and generated PDFs stay on your computer under the CV++ user configuration directory. The local Go process sends credentials directly to IIT KGP ERP. CV++ has no hosted backend, telemetry, analytics, or automatic upload.

## The four-step workflow

1. Download the release for Windows, macOS Apple Silicon/Intel, or Linux from the [CV++ Releases](https://github.com/shikharish/cvpp/releases/latest) page.
2. Double-click CV++ and enter your ERP login in the local wizard. The emailed OTP is entered in the browser.
3. CV++ imports the current `StudentForm.jsp` without posting resume fields. Edit with local autosave and see the current CV1 PDF beside the workspace.
4. Press **Update ERP Resume** when you explicitly want to synchronize ERP. The PDF refreshes automatically after a successful download.

The first import is download-only with respect to resume fields. A timestamped backup is created before replacing an existing local JSON file. Failed imports leave the existing local resume untouched.

## Local data layout

CV++ uses `os.UserConfigDir()/cvpp` (for example, `%AppData%/cvpp` on Windows and `~/Library/Application Support/cvpp` on macOS):

- `data/resume.json` — canonical local resume
- `secrets/erpcreds.json` and `secrets/.session` — permission-restricted credentials/session
- `pdf/resume-erp-cv1.pdf` through `cv3` — local portal PDFs
- `backups/` — timestamped resume recovery copies
- `runtime/` and `logs/` — short-lived local process state and diagnostics

Use **Forget ERP login** in Advanced and privacy to remove credentials and the ERP session while preserving resume content.

## Unsigned downloads

CV++ releases are currently unsigned. Windows may require **More info → Run anyway** once in SmartScreen. On macOS, right-click the app and choose **Open** once in Gatekeeper. Linux users may need to mark the AppImage executable. Signing is planned; never bypass a warning for a file that was not downloaded from the official release.

## Advanced and development

The `cvpp editor` and `cvpp erp` commands remain available for maintainers and power users. They retain repository-relative working-directory behavior and support `--data-dir` for isolated tests. `--cv 1|2|3`, `--download-only`, `--fresh-login`, JSON import/export, portal snapshot import, logs, and the watcher are advanced tools; ordinary students should use the app button and local autosave.

Run the test suite from this directory with `go test ./...`. CI uses fake ERP fixtures only and never stores real credentials.

## Links

- [Download CV++](https://github.com/shikharish/cvpp/releases/latest)
- [Source and issues](https://github.com/shikharish/cvpp)
- [Privacy statement](PRIVACY.md)
- [MIT License](LICENSE)
