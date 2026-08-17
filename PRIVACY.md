# CV++ privacy

CV++ is local-first. Credentials, security-question answers, ERP session data, resume JSON, backups, logs, and PDFs are written to the computer running CV++. The local process communicates directly with IIT Kharagpur ERP when you request a question, login, import, download, or explicit update.

The GitHub Pages website only contains documentation and links to release assets. It has no credential form, localhost bridge, analytics, cookies, or ERP requests. CV++ does not operate a hosted API and does not transmit resume content to the project maintainers.

The local API binds to `127.0.0.1` on a random port, uses a one-time bootstrap token and an HttpOnly SameSite cookie, rejects unexpected hosts/origins, and applies no-cache and restrictive browser headers. Passwords, answers, OTPs, and ERP session tokens are never returned by status endpoints and are redacted from diagnostics.
