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

Use the setup script. It is the supported setup path and does the required local setup:

- creates `data/resume.json`
- creates `.erp-cv-secrets/erpcreds.json`
- asks for your ERP roll number and password
- fetches your current ERP security question when possible
- asks for the security-question answer
- builds `./erp-cv-portal`

If setup cannot fetch the security question automatically, it will ask you to type the question text manually. The text must match ERP exactly. If ERP later reports a different question, run setup again and add that question too.

If setup fails or your ERP credentials change, run it again:

```sh
./scripts/setup.sh
```

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

## Safety

The following are ignored by git:

- `.erp-cv-secrets/`
- `data/resume.json`
- `pdf/`
- generated PDFs
- saved/debug ERP pages

Do not commit your real ERP credentials, security answers, session files, or generated CV PDFs.
