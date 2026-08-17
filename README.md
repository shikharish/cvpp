# IIT KGP ERP CV Portal

A local editor and CLI for maintaining IIT Kharagpur ERP CV data in JSON, syncing it to the CDC resume portal, and downloading the portal-generated PDF.

This project is meant to be cloned and run locally by each student. Your ERP credentials, session, resume JSON, and generated PDFs stay on your machine and are ignored by git.

## Requirements

- Go installed
- IIT KGP ERP access
- Access to the email OTP sent by ERP
- A browser for the local editor

## Quick setup

```sh
git clone <repo-url>
cd erp-cv-portal
./scripts/setup.sh
```

Then edit:

```text
.erp-cv-secrets/erpcreds.json
data/resume.json
```

Build manually if you do not use the setup script:

```sh
go build -o erp-cv-portal ./cmd/erp-cv-portal
```

## Configure ERP credentials

Copy the example if the setup script has not already done it:

```sh
mkdir -p .erp-cv-secrets
cp config/erpcreds.example.json .erp-cv-secrets/erpcreds.json
```

Edit `.erp-cv-secrets/erpcreds.json`:

```json
{
  "roll_number": "23XX00000",
  "password": "YOUR_ERP_PASSWORD",
  "answers": {
    "Exact security question shown by ERP": "YOUR_SECURITY_ANSWER"
  }
}
```

The security-question key must match the ERP question text exactly. If the tool says the returned question is missing, add that exact question and answer to the `answers` object.

## Create your resume JSON

```sh
cp data/resume.example.json data/resume.json
```

Then start the editor:

```sh
./erp-cv-portal editor
```

The editor runs on localhost, loads `data/resume.json`, and saves edits back to the same file.

## Sync to ERP and download PDF

```sh
./erp-cv-portal erp --cv 1
```

What happens:

1. The tool logs in to ERP.
2. ERP sends an email OTP.
3. Paste the OTP when the terminal asks for it.
4. The tool syncs the managed CV fields.
5. The tool downloads the portal-generated PDF to `pdf/resume-erp-cv1.pdf`.

Other useful commands:

```sh
./erp-cv-portal erp --cv 2
./erp-cv-portal erp --cv 3
./erp-cv-portal erp --download-only --cv 1
./erp-cv-portal erp --fresh-login --cv 1
./erp-cv-portal erp --open
```

## Editor workflow

```sh
./erp-cv-portal editor
```

Use the sidebar to:

- Save `data/resume.json`
- Open a JSON file
- Import a saved portal snapshot
- Sync and download an ERP PDF
- Open the local PDF viewer
- Open ERP in a browser handoff

Hidden entries and hidden bullet points remain in JSON but are skipped during ERP sync.

## Auto-sync CV1 when JSON changes

```sh
./scripts/watch-cv1.sh
```

This watches `data/resume.json` and runs:

```sh
./erp-cv-portal erp --cv 1
```

Stop it with `Ctrl-C`.

## Optional Gmail OTP automation

Manual OTP is the default and recommended setup.

If `.erp-cv-secrets/client_secret.json` and `.erp-cv-secrets/.token` exist, the tool will try to read the OTP from Gmail automatically. If that fails, it falls back to manual OTP entry.

## Safety notes

The following are ignored by git:

- `.erp-cv-secrets/`
- `data/resume.json`
- `pdf/`
- generated PDFs
- saved/debug ERP pages

Do not commit your real ERP credentials, security answers, session files, or generated CV PDFs.
