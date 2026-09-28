# Lightweight Email Client — Action Plan

Design and Implementation of a Lightweight Email Client
Computer Network Course Practice · 2026 Fall

**Stack:** Go (client core) + Node.js/Tailwind (web UI)
**Test server:** GreenMail (local, Docker)

---

## 1. Project Overview

Single-account, plain-text email client over raw TCP sockets:

- **Send** — SMTP (HELO/EHLO, MAIL FROM, RCPT TO, DATA, QUIT)
- **Receive** — POP3 (USER, PASS, STAT/LIST, RETR, QUIT)
- **Inbox** — message number, sender, subject, date; select and read one
- **Local mailbox** — save downloaded messages, reload after restart
- **Logging** — timestamped events, passwords never logged

Grading: **Report 40% · Code 20% · Presentation 10% · Performance 30%**.
Correctness first, features second.

---

## 2. Key Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Language | **Go** | Compiled systems language (spirit of "C/C++/Java encouraged"), stdlib networking (`net`, `textproto`) is ideal, practices Go as requested |
| Test server | **GreenMail via Docker** | Trusted reference SMTP + POP3 + IMAP implementation; test against a real server, not code you wrote |
| UI | **Web UI (bonus)** + thin CLI | CLI satisfies the spec minimum and the "terminal output" deliverable; web UI earns the GUI bonus and helps the demo |
| Go ↔ UI link | **Go is the only backend** | `net/http` JSON API; Node.js is only the Vite/Tailwind build toolchain — no Express middle tier |

**Important Go note:** use `net/textproto` (stdlib line-based protocol helper). It does *not* hide SMTP/POP3 semantics, so it is within spec:

- `textproto.DotReader` / `DotWriter` handle the `\r\n.\r\n` terminator for SMTP DATA and POP3 RETR, plus automatic dot-stuffing on send.
- `conn.ReadResponse(250)` waits for and validates a 3-digit SMTP reply code.

Do **not** use `net/smtp` — it is a full SMTP client and the spec forbids it.

---

## 3. Architecture

```
NetworkLab/
├── Client/                  # Go — networking + state + API
│   ├── go.mod               # module email-client
│   ├── config.json          # server addr, smtp_port, pop3_port, username
│   ├── cmd/
│   │   ├── cli/             # CLI menu (spec minimum UI)
│   │   └── serve/           # HTTP JSON API (powers the web UI)
│   └── pkg/                 # not internal/: Tests/ must be able to import it
│       ├── config/          # load + validate config
│       ├── smtp/            # SMTP client (raw socket + textproto)
│       ├── pop3/            # POP3 client (raw socket + textproto)
│       ├── message/         # model + header parse (net/mail)
│       ├── mailbox/         # local storage (encoding/json)
│       ├── logger/          # log/slog + password redaction
│       └── server/          # net/http JSON handlers
├── Tests/                   # Go module email-client-tests (replace => ../Client)
└── WebUI/                   # Node.js + Tailwind — the GUI
    ├── package.json
    ├── vite.config.js       # proxy /api -> :8080
    ├── tailwind.config.js
    ├── index.html
    └── src/
```

**Core design principle:** `cmd/cli` and `cmd/serve` are thin frontends over the same
`internal/` core (smtp, pop3, mailbox, message). The networking/protocol logic lives in
exactly one place — this is also what the report's "Design" section should show.

Go idioms to use: `log/slog` (structured logging; `ReplaceAttr` for clean password
redaction), `encoding/json` for the mailbox, `net.DialTimeout` + `conn.SetDeadline` for
timeouts, idiomatic `if err != nil` + `fmt.Errorf("smtp: %w", err)`. Core stays
single-threaded; goroutines only for the "background check" or GUI bonus.

---

## 4. API Contract (the seam between the two folders)

```
GET  /api/inbox         -> [{id, from, subject, date}]
POST /api/refresh       -> pull POP3 into mailbox, return count
GET  /api/messages/:id  -> {id, from, to, subject, date, body}
POST /api/send          -> {to:[], subject, body}   (SMTP)
GET  /api/local         -> locally saved messages
GET  /api/health        -> server + config status
```

- **Dev:** Vite proxies `/api` → `:8080`, so no CORS headache.
- **Demo/production:** Go serves the built web UI via `go:embed` — single process, zero cross-origin.

---

## 5. Action Plan — Module Blocks

Build in this order. Each block has a "done when" acceptance criterion.

### BLOCK 0 · Test server up
Run GreenMail; verify SMTP `:3025` + POP3 `:3110` by hand (EHLO → 250, USER/PASS → +OK).
Confirm demo credentials, write `config.json`.

```bash
docker run -d --name greenmail -p 3025:3025 -p 3110:3110 greenmail/standalone
```

**Done when:** a raw telnet/nc handshake succeeds on both ports.

### BLOCK 1 · Scaffold + config + logger
`go.mod`, folder skeleton, `internal/config` (load/validate), `internal/logger`
(slog + password redaction).

**Done when:** `go build ./...` clean; a test proves the logger never emits the password.

### BLOCK 2 · smtp package
EHLO / MAIL FROM / RCPT TO / DATA / QUIT via `net.DialTimeout` + `textproto.DotWriter`;
parse 3-digit reply codes; per-socket deadline.

**Done when:** a message lands in GreenMail and QUIT returns 221.

### BLOCK 3 · pop3 package
USER / PASS / STAT / LIST / RETR / QUIT via `textproto.DotReader`; handle `+OK`/`-ERR`
and multi-line body.

**Done when:** LIST returns the messages you sent; RETR returns full raw text.

### BLOCK 4 · message + mailbox
`internal/message` (net/mail header parse), `internal/mailbox` (JSON persistence,
monotonic local id separate from POP3 number).

**Done when:** a retrieved message parses to From/Subject/Date/body; save + reload survives a restart.

### BLOCK 5 · CLI frontend (cmd/cli)
The 5-item spec menu wired to smtp/pop3/mailbox.

**Done when:** full send → receive → read → local-view loop works from the terminal.
(This also produces the "terminal output" deliverable.)

### BLOCK 6 · HTTP API (cmd/serve + internal/server)
The endpoints above as thin `net/http` handlers over the same core.

**Done when:** `curl /api/send` and `curl /api/inbox` round-trip against GreenMail.

### BLOCK 7 · webui scaffold
Vite + Tailwind project, Vite proxy → `:8080`, base layout + routing.

**Done when:** `npm run dev` serves a styled page and `/api/health` shows connected.

### BLOCK 8 · webui screens
Inbox, Read message, Compose+Send, Local messages — dark, data-forward, minimal text.

**Done when:** full email flow runs from the browser.

### BLOCK 9 · Error handling + logging pass
Map all six failure modes (connection refused, timeout, bad auth, unexpected response,
mid-transfer disconnect, invalid input) to clean messages in both CLI and API; verify
redaction.

**Done when:** 3+ error cases demo cleanly, no crash, no leaked password.

### BLOCK 10 · Testing + performance (§5)
Connect both servers; send 3+; retrieve + display; save + reopen; 3 error cases;
timing table (send/retrieve ms).

**Done when:** the spec's §5 checklist is ticked and the timing table is filled.

### BLOCK 11 · Report + README + deliverables
Report (10 template sections — 40% of grade), README (build/config/run for both folders),
screenshots + terminal output + logs, bonus-feature note.

**Done when:** every §8 deliverable is assembled.

---

## 6. Dependency Notes

- **Critical path:** Blocks 2 + 3 (smtp/pop3) — everything hangs off them. Get these solid before touching any UI.
- **Parallel track:** once Block 6 is done, Blocks 7–8 (web UI) can run in parallel with Block 9 (error handling).
- Blocks are otherwise linear; do not start the report (Block 11) until testing (Block 10) is green.

---

## 7. Report Structure (template §7 — 40% of grade)

1. Requirements
2. Analysis
3. Design
4. Development and Implementation
5. System Deployment, Startup, and Use
6. System Test
7. Performance (the timing table)
8. Summary or Conclusions
9. References (cite RFC 5321 SMTP, RFC 1939 POP3, and libraries actually used)
10. Comments (optional)

Focus the report on: implemented SMTP/POP3 protocol flow, TCP stream handling, program
structure, normal + error-case tests, and the brief performance measurement.

---

## 8. Deliverables (spec §8)

- [ ] Complete source code (both `client/` and `webui/`)
- [ ] README — compilation, configuration, execution
- [ ] One report (course template)
- [ ] Screenshots / terminal output of main functions
- [ ] Test results + representative log files
- [ ] Short description of bonus features

---

## 9. Bonus Features (after core is solid — pick 1–2, quality over quantity)

TLS/STARTTLS · MIME attachments · background mail checking · IMAP · multiple accounts ·
local search · read/unread + delete · HTML email · GUI (web) · improved concurrency.

The web UI itself is already a listed bonus ("Graphical interface").
