# Lightweight Email Client

Computer Network Course Practice · 2026 Fall · Beijing Institute of Technology

A single-account, plain-text email client in Go. It sends mail with SMTP and receives it with
POP3. Both protocols are written by hand over raw TCP sockets. Downloaded messages are kept in
a local JSON mailbox, so they are still there after a restart. There is a command-line menu
(the required interface) and an optional web interface.

The report is in [Documents/report/report.pdf](Documents/report/report.pdf). Test evidence is in
[Results/](Results/README.md).

## Contents

| Folder | What it is |
|---|---|
| `Client/` | The Go program (module `email-client`): `cmd/cli` (terminal menu), `cmd/serve` (HTTP API + web UI host), and `pkg/` (smtp, pop3, message, mailbox, account, config, logger, mailerr, server) |
| `Tests/` | Go module `email-client-tests`: unit, integration, CLI, API and performance tests, plus `section5.sh` |
| `WebUI/` | Web interface (bonus): plain JavaScript, built with Vite + Tailwind CSS |
| `Results/` | Transcripts, log, saved mailbox, timing table and screenshots from the §5 test run |
| `Documents/` | Assignment description, report template, and the report (`report/`) |

## Requirements

- **Go 1.26.7** or newer (the version in `go.mod`)
- **Docker**, to run the GreenMail test mail server
- **Node.js** (developed with 25.7), only to build the web interface

## 1. Start the test mail server

[GreenMail](https://greenmail-mail-test.github.io/greenmail/) provides SMTP on port 3025 and
POP3 on port 3110, with two test accounts:

```bash
docker run -d --name greenmail -p 3025:3025 -p 3110:3110 \
  -e GREENMAIL_OPTS="-Dgreenmail.setup.test.all -Dgreenmail.hostname=0.0.0.0 \
    -Dgreenmail.users=alice:alice@localhost,bob:bob@localhost \
    -Dgreenmail.users.login=email -Dgreenmail.verbose" \
  greenmail/standalone
```

| Account | Password |
|---|---|
| `alice@localhost` | `alice` |
| `bob@localhost` | `bob` |

These are GreenMail test credentials, not real ones. To check that the server is up, run
`nc localhost 3025`. It should answer with `220 ... GreenMail SMTP Service ... ready`.

## 2. Configure

`Client/config.json`:

```json
{
  "server": "localhost",
  "smtp_port": 3025,
  "pop3_port": 3110,
  "username": "alice@localhost",
  "password": "alice",
  "from": "alice@localhost",
  "timeout_seconds": 10,
  "mailbox_path": "data/mailbox.json",
  "log_path": "logs/client.log"
}
```

| Key | Meaning |
|---|---|
| `server`, `smtp_port`, `pop3_port` | Where the mail server listens |
| `username` / `password` | POP3 login. You can leave `password` out of the file and set `EMAIL_PASSWORD` instead |
| `from` | Address used in `MAIL FROM` and in the `From:` header |
| `timeout_seconds` | Time limit for each network step (default 10) |
| `mailbox_path`, `log_path` | Local mailbox and log file, relative to the folder you run from |

The program checks the file at startup and lists every problem at once. A misspelled key such
as `smpt_port` is also reported as an error.

## 3. Build and run the CLI

```bash
cd Client
go build ./...                      # compile everything
go run ./cmd/cli                    # start the menu
go run ./cmd/cli -debug             # also log every protocol line (C: / S:)
go run ./cmd/cli -config other.json # use another config file
EMAIL_PASSWORD=alice go run ./cmd/cli   # password from the environment
```

To build a binary, run `go build -o emailcli ./cmd/cli` and then `./emailcli`.

The menu:

```
1. Check Inbox            download the inbox (LIST + RETR), save new messages, show a table
2. Read Message           retrieve one message by its number from the table
3. Compose and Send Email recipients (comma-separated), subject, body; end the body with a line "."
4. View Local Messages    saved messages, no network needed; open one by local ID
5. Exit
```

`data/` and `logs/` are created in the folder you run from (`Client/` above). The log file is
`logs/client.log`. Passwords are never written to it; the `PASS` command is logged as
`PASS ****`.

## 4. Run the web interface (bonus)

Build the web interface once, then let the Go server host it:

```bash
cd WebUI  && npm install && npm run build      # writes WebUI/dist/
cd Client && go run ./cmd/serve -web ../WebUI/dist
```

Open <http://127.0.0.1:8081> and sign in with one of the GreenMail accounts. Each account
has its own saved mailbox under `Client/data/accounts/<email>/`.

To work on the web interface with live reload, use two terminals:

```bash
cd Client && go run ./cmd/serve       # API on :8081
cd WebUI  && npm run dev              # http://localhost:5173, proxies /api to :8081
```

`serve` flags: `-addr` (default `127.0.0.1:8081`), `-web <folder>`, `-config`, `-debug`. By
default it listens only on localhost, because the session cookie is sent over plain HTTP.

## 5. Tests

```bash
cd Tests
go test ./...              # all tests (integration tests are skipped if GreenMail is not running)
go test -race ./...        # same, with the race detector
PERF=1 go test ./perf -v   # timing table -> Results/timing.md (sends ~30 messages to bob)
```

To regenerate all the §5 evidence in `Results/` (transcripts, log check, timing, test list):

```bash
Tests/section5.sh --reset         # restarts GreenMail first, so the inbox starts empty
Tests/section5.sh --no-go-tests   # skip the Go suite
```

`--reset` runs `docker restart greenmail`, so it expects the container from step 1.

## Features

**Required:**
- SMTP: `EHLO` (falls back to `HELO`), `MAIL FROM`, `RCPT TO` (one or more recipients),
  `DATA`, `QUIT`
- POP3: `USER`/`PASS`, `STAT`, `LIST`, `RETR`, `QUIT`
- Inbox showing number, sender, subject and date, and a message viewer
- Local JSON mailbox that survives restarts, with no duplicate saves
- Clear messages for errors: server unreachable, timeout, wrong password, server refusal,
  unexpected reply, interrupted transfer, invalid input
- Event log with no passwords in it

**Bonus:**
- **Graphical interface:** a web UI with Inbox, Read, Compose and Saved screens, a live
  SMTP/POP3 status light, a light/dark theme, and a layout that works on phones
- **Multiple accounts:** sign in with any account on the server; the password is checked with
  POP3 `USER`/`PASS`, and each account has its own mailbox and profile picture
- **Safe concurrency:** the HTTP server handles requests in parallel; shared state is guarded
  by locks and checked with `go test -race`

**Not implemented:** TLS/STARTTLS, SMTP AUTH, attachments, IMAP, HTML mail, deleting mail on
the server.

## About the protocol code

SMTP and POP3 are not implemented with `net/smtp` or any other mail library. The client uses
the Go standard library's `net/textproto` only to read and write lines and dot-terminated
blocks. Every command, the order they are sent in, and every reply check are written in
`pkg/smtp` and `pkg/pop3`.
