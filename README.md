# CV++

A local desktop editor for IIT Kharagpur ERP resumes. CV++ imports your existing ERP resume, autosaves edits locally, and updates ERP only when you press **Update ERP Resume**.

## Install

Download the latest build from [GitHub Releases](https://github.com/shikharish/cvpp/releases/latest), unzip it, and open CV++.

- Windows: `CV++.exe`
- macOS: choose the Apple Silicon or Intel build
- Linux: `CV++-linux-amd64.AppImage`

Releases are currently unsigned. Windows SmartScreen and macOS Gatekeeper may require a one-time confirmation.

## Use

1. Enter your ERP login and the emailed OTP.
2. CV++ imports your existing ERP resume without changing it.
3. Edit locally with autosave.
4. Press **Update ERP Resume** to sync and refresh the ERP PDF.

Credentials, sessions, resume data, backups, and PDFs stay on your computer. CV++ has no hosted backend, telemetry, or analytics. See [PRIVACY.md](PRIVACY.md) for details.

## Development

```sh
go test ./...
go run ./cmd/cvpp
```

Advanced commands are documented in [docs/advanced-cli.md](docs/advanced-cli.md).

[Issues](https://github.com/shikharish/cvpp/issues) · [MIT License](LICENSE)
