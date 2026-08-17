# IIT KGP ERP CV Portal

A local editor and CLI for maintaining IIT Kharagpur ERP CV data in JSON, syncing it to the CDC resume portal, and downloading the portal-generated PDF.

Your ERP credentials, session, resume JSON, and generated PDFs stay on your machine and are ignored by git.

## Requirements

- Go installed
- IIT KGP ERP access
- Access to the email OTP sent by ERP
- A browser for the local editor

## Setup

```sh
git clone <repo-url>
cd erp-cv-portal
./scripts/setup.sh
```

The setup script does the required local setup:

- creates `data/resume.json`
- creates `.erp-cv-secrets/erpcreds.json`
- asks for your ERP roll number and password
- fetches your current ERP security question when possible
- asks for the security-question answer
- builds `./erp-cv-portal`

If setup cannot fetch the security question automatically, it will ask you to type the question text manually. The text must match ERP exactly. If ERP later reports a different question, run setup again and add that question too.

## Recommended workflow

Use two terminals.

Terminal 1:

```sh
./scripts/watch-cv1.sh
```

Terminal 2:

```sh
./erp-cv-portal editor
```

Keep Terminal 1 open while editing. Every time `data/resume.json` is saved, the watcher runs:

```sh
./erp-cv-portal erp --cv 1
```

Then it downloads:

```text
pdf/resume-erp-cv1.pdf
```

When ERP asks for OTP, paste it in Terminal 1.

## What to edit

Use the editor for normal work:

```sh
./erp-cv-portal editor
```

The editor loads and saves:

```text
data/resume.json
```

Hidden entries and hidden bullet points remain in JSON but are skipped during ERP sync.

## Advanced commands

Most students should only need the recommended workflow above.

Manual one-time sync:

```sh
./erp-cv-portal erp --cv 1
```

Other CV variants:

```sh
./erp-cv-portal erp --cv 2
./erp-cv-portal erp --cv 3
```

Download the currently saved ERP PDF without syncing JSON:

```sh
./erp-cv-portal erp --download-only --cv 1
```

Force a new ERP login:

```sh
./erp-cv-portal erp --fresh-login --cv 1
```

Open ERP in the browser:

```sh
./erp-cv-portal erp --open
```

## Optional Gmail OTP automation

Manual OTP is the default and recommended setup.

If `.erp-cv-secrets/client_secret.json` and `.erp-cv-secrets/.token` exist, the tool will try to read the OTP from Gmail automatically. If that fails, it falls back to manual OTP entry.

## Safety

The following are ignored by git:

- `.erp-cv-secrets/`
- `data/resume.json`
- `pdf/`
- generated PDFs
- saved/debug ERP pages

Do not commit your real ERP credentials, security answers, session files, or generated CV PDFs.
