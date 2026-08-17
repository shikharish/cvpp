# Secrets directory

Create `.erp-cv-secrets/` in the project root. This directory is ignored by git.

Required:

```text
.erp-cv-secrets/erpcreds.json
```

Optional advanced Gmail OTP files:

```text
.erp-cv-secrets/client_secret.json
.erp-cv-secrets/.token
```

The normal setup path does not need Gmail OAuth. If the optional Gmail files are absent, the CLI requests an ERP OTP and asks you to paste it in the terminal.
