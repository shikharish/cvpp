# Advanced CLI

The double-click app is the supported student workflow. Maintainers can run the same binary from a checkout:

```sh
go run ./cmd/cvpp editor [--data-dir PATH] [--no-open]
go run ./cmd/cvpp erp --cv 1 [--data-dir PATH] [--download-only] [--fresh-login]
```

`cvpp editor` serves the embedded browser editor and keeps the current working-directory behavior for repository-relative JSON, PDF, and secret paths. `--data-dir` opts into the portable application layout for isolated tests. `cvpp erp` retains terminal OTP support for advanced workflows; it is explicit authorization to write managed ERP fields unless `--download-only` is supplied.

The JSON editor, portal snapshot importer, watcher, logs, CV2/CV3 selection, and fresh-login controls are intentionally advanced. Never commit credentials, sessions, recovery backups, or generated PDFs.
