# Code Walkthrough — Blocks 0 to 4

This file explains the code block by block, so the source itself can stay light on comments.
Read it next to the source files. Headings follow the file order.

```
NetworkLab/
├── Client/                 module email-client (the program)
│   ├── config.json
│   └── pkg/
│       ├── config/         load + validate config.json
│       ├── logger/         slog with password redaction
│       ├── smtp/           SMTP client over a raw TCP socket
│       ├── pop3/           POP3 client over a raw TCP socket
│       ├── message/        build outgoing / parse incoming mail text
│       └── mailbox/        JSON file of downloaded messages
└── Tests/                  module email-client-tests (all tests)
```

**Why `pkg/` and not `internal/` as in the plan:** Go only allows code inside `Client/` to import
`Client/internal/...`. The tests live in a separate `Tests/` folder, so the packages have to be
importable from outside. `Tests/go.mod` has `replace email-client => ../Client`, which makes
`import "email-client/pkg/smtp"` resolve to the local folder instead of the internet.

Run everything with:

```bash
cd Client && go build ./...      # compiles the client
cd Tests  && go test ./...       # runs all tests
```

---

## Block 0 · `Client/config.json`

```json
"server": "localhost", "smtp_port": 3025, "pop3_port": 3110,
"username": "alice@localhost", "password": "alice", "from": "alice@localhost",
"timeout_seconds": 10, "mailbox_path": "data/mailbox.json", "log_path": "logs/client.log"
```

- `3025` / `3110` are GreenMail's default test ports (the real ones are 25 and 110, which need root).
- The GreenMail Docker image starts with authentication disabled. A user is created the first
  time someone logs in or receives mail, and the login name is the full address
  (`alice@localhost`). Any password is accepted. Check this during the Docker step.
- `from` is the address used in `MAIL FROM` and in the `From:` header.
- `password` can be left out of the file and supplied with `EMAIL_PASSWORD=... ` instead
  (see the config package).

**Still to do for Block 0 (Docker postponed):** start GreenMail and do the hand handshake:

```bash
docker run -d --name greenmail -p 3025:3025 -p 3110:3110 greenmail/standalone
nc localhost 3025     # expect "220 ...", type: EHLO me   -> 250 lines, then QUIT -> 221
nc localhost 3110     # expect "+OK ...", type: USER alice@localhost / PASS alice -> +OK, QUIT
```

---

## Block 1a · `pkg/config/config.go`

**`PasswordEnv` const.** Name of the environment variable that overrides the password. It lets
you keep the real password out of a file that might be committed or handed in.

**`Config` struct.** One field per JSON key. The `json:"smtp_port"` tags map the snake_case
keys in the file to Go field names.

**`Load`, reading and decoding.**
- `os.ReadFile` reads the whole file. A missing file becomes an error prefixed `config:`.
- `json.NewDecoder(...).DisallowUnknownFields()` makes a typo like `"smpt_port"` an error
  instead of being silently ignored, which would leave the port at 0.

**`Load`, env override and defaults.** `os.LookupEnv` distinguishes "not set" from "set to
empty". Then timeout, mailbox path and log path get defaults if the file left them out.

**`Load`, validation.** Calls `Validate` so a bad config never reaches the network code.

**`Validate`.** Collects *every* problem into `errs` and combines them with `errors.Join`, so
the user sees all mistakes at once instead of fixing them one run at a time. Checks: server
not blank, both ports in 1–65535, username not blank, `from` contains `@`, timeout not negative.

**`SMTPAddr` / `POP3Addr`.** `net.JoinHostPort` builds `"host:port"`. It also brackets IPv6
addresses correctly (`[::1]:3025`), which plain string concatenation would get wrong.

**`Timeout`.** Converts the integer seconds into a `time.Duration` for the socket code.

---

## Block 1b · `pkg/logger/logger.go`

**`secretKeys` map.** Attribute names whose values must never be printed.

**`New`.** Builds a `log/slog` logger with the text handler. Output looks like
`time=... level=INFO msg="Connected to SMTP server" addr=localhost:3025`. `Level` filters
out lower levels (for example, Debug protocol traces are hidden at Info).

**`ReplaceAttr: redact`.** slog calls `redact` for every key/value pair before writing it.
If the key is `password`, `pass` or `secret` (case-insensitive), the value is swapped for
`[REDACTED]`. This works even if a later change passes a password to the log by mistake.

**`Open`.** Creates the log folder (`MkdirAll`), opens the file in append mode so old runs
are kept, with permission `0600` (owner only). Returns the file as an `io.Closer` so the
caller can close it on exit.

Key-based redaction cannot catch a password pasted into a message string. That case is
handled where it can happen: the POP3 `PASS` command (see Block 3).

---

## Block 2 · `pkg/smtp/smtp.go`

**`Client` struct.**
- `conn`: the raw TCP socket (`net.Conn`).
- `tp`: `textproto.Conn` wrapped around the same socket. It does line-level work only: read a
  line up to `\r\n`, write a line with `\r\n`, parse 3-digit replies. The SMTP commands and
  their order are all written by us.
- `timeout`: how long any single step may take.

**`Dial`.**
1. `net.DialTimeout("tcp", addr, timeout)` opens the TCP connection. It fails with
   "connection refused" if nothing listens, or times out if the host does not answer.
2. `extend()` sets the socket deadline before the first read.
3. `tp.ReadResponse(220)` reads the server greeting. `220` is "service ready". Any other
   code (e.g. `554` "no service") returns a `*textproto.Error` with the code and text.
4. The reply is logged at Debug as `S: ...`, and an Info event "Connected to SMTP server".

**`extend`.** `SetDeadline(now + timeout)` puts a time limit on every read and write on the socket.
Calling it before each step gives each step a fresh full timeout, so a slow but working
server is fine while a stuck one fails instead of hanging forever.

**`cmd`.** The helper every command uses: format the line, extend the deadline, log
`C: <line>`, send it with `PrintfLine` (adds `\r\n` and flushes), then `ReadResponse(expect)`.
`ReadResponse` reads a *complete* reply, including multi-line replies like
`250-first` / `250 last`, so a reply split across several TCP reads is still handled. This is
the "single socket read is not one response" issue from the spec.

**`Hello`.** Sends `EHLO`. If the server answers 5xx (it does not know EHLO), it falls back to
the older `HELO`. `errors.As` pulls the `*textproto.Error` out of the wrapped error to read
its code.

**`Mail` / `Rcpt`.** `MAIL FROM:<addr>` and `RCPT TO:<addr>`, each expecting `250`.
The angle brackets are required by RFC 5321. `Rcpt` names the rejected address in its error.

**`Data`.**
1. `DATA` must get `354` ("start mail input").
2. `tp.DotWriter()` returns a writer that:
   - turns each `\n` into `\r\n`,
   - adds an extra `.` to any line starting with `.` (dot-stuffing), so a body line `.` is
     not mistaken for the end marker,
   - writes the final `.\r\n` on `Close()`.
3. After `Close`, `ReadResponse(250)` waits for the server to accept the message.

**`Quit`.** `QUIT` expects **221** ("closing"), not 250. RFC 5321 §4.2.2 fixes this code;
the plan's "QUIT returns 250" should read 221. The socket is closed whether or not the reply
was good.

**`Close`.** Plain socket close, used by `defer` in `Send` so the connection is released on
every error path. Closing twice after `Quit` is harmless.

**`Send`.** One whole session in the order the spec lists: connect → EHLO → MAIL FROM →
RCPT TO (once per recipient, so several recipients work) → DATA → QUIT. Any failure returns
right away with an error that says which step failed, e.g.
`smtp: RCPT TO ghost@x: 550 no such user`.

---

## Block 3 · `pkg/pop3/pop3.go`

POP3 replies differ from SMTP: a status line starting with `+OK` or `-ERR`, no numeric codes.
So `ReadResponse` is not used here; `reply` parses the line itself.

**`Error` type.** Represents a `-ERR` answer. Having a type lets callers tell "server said no"
(wrong password, no such message) apart from network failures, using `errors.As`.

**`MsgInfo`.** One LIST line: message number and size in bytes.

**`Dial`.** Same TCP connect as SMTP, then reads the greeting with `reply("greeting")`, which
must be `+OK`.

**`reply`.** Extends the deadline, reads one line (`ReadLine` strips `\r\n`), logs it, then
`strings.Cut(line, " ")` splits it into status and text:
- `+OK` → return the text (e.g. `"2 320"` for STAT)
- `-ERR` → return `*Error`
- anything else → "unexpected response" error. This covers talking to a non-POP3 server.

**`cmd`.** Sends one command and reads its status line. The command name is the first word.
If it is `PASS`, the log gets `C: PASS ****` while the real line goes to the socket. This is
the second half of the "never log the password" rule.

**`Auth`.** `USER name` then `PASS password`. Either failing writes the spec's
`POP3 authentication failed` event at Error level and returns the error.

**`Stat`.** `STAT` answers `+OK <count> <total-bytes>`; `Sscanf` reads the two numbers.

**`List`.** `LIST` answers `+OK` then one line per message, then a line with just `.`.
`ReadDotLines` reads until that `.` and returns the lines between. Each is parsed as
`<num> <size>`.

**`Retr`.** `RETR n`, then `io.ReadAll(tp.DotReader())`. The DotReader is the reverse of
SMTP's DotWriter: it reads until the lone `.` line, removes dot-stuffing (`..x` → `.x`) and
converts `\r\n` to `\n`. If the socket drops before the `.` arrives, `ReadAll` returns
`unexpected EOF`, which is reported as "transfer interrupted". That is the spec's
interrupted-transfer case.

**`Quit` / `Close`.** Same pattern as SMTP. `QUIT` also tells the server to commit the
session (in POP3 this is when messages marked for deletion are removed).

---

## Block 4a · `pkg/message/message.go`

**`Message` struct.** The fields the Inbox shows: From, To, Subject, Date, plus the Body.
JSON tags are used when the mailbox saves it.

**`Parse`.**
1. `mail.ReadMessage` (standard library, RFC 5322) splits the headers from the body. It only
   parses text; it does no networking.
2. `io.ReadAll(m.Body)` reads the body.
3. `header` closure: gets a header and decodes MIME encoded-words, so
   `=?utf-8?q?caf=C3=A9?=` becomes `café`. If decoding fails, the raw value is kept.
4. `m.Header.Date()` parses the Date header. If it is missing or malformed the error is
   ignored and `Date` stays at the zero time, and the UI can print "unknown".

**`Build`.** Writes the text that goes after `DATA`:
- `From`, `To` (recipients joined with `, `), `Subject`, `Date` in RFC 1123Z format.
- `mime.QEncoding.Encode` encodes the subject only when it has non-ASCII characters or
  control characters. A side effect is safety: a subject containing a newline
  (`"hi\nBcc: x"`) is encoded instead of creating a second header line.
- `MIME-Version`, `Content-Type: text/plain; charset=utf-8` and
  `Content-Transfer-Encoding: 8bit` tell the receiver how to read the body.
- A blank line separates headers from body.
- The body's `\r\n` becomes `\n` and a final newline is added. The SMTP DotWriter later turns
  every `\n` into `\r\n`, so line endings are consistent on the wire.

---

## Block 4b · `pkg/mailbox/mailbox.go`

**`Entry`.** One saved message.
- `ID`: local number, counts up forever and is never reused.
- `POP3Num`: the number the server used when it was downloaded. POP3 numbers are only valid
  inside one session (message 1 today may be a different mail tomorrow), which is why the
  local ID is separate.
- `Hash`: SHA-256 of the raw text, used to detect duplicates.
- `message.Message` is *embedded*, so its fields (`From`, `Subject`, ...) appear directly on
  `Entry` and in the JSON.
- `Raw`: the full original text, kept so nothing is lost if parsing is improved later.

**`file` / `Mailbox`.** `file` is what goes to disk (`next_id` plus the list). `Mailbox`
wraps it with the path and keeps the data unexported so callers must go through the methods.

**`Open`.** A missing file is a normal first run, so it returns an empty mailbox with
`NextID: 1`. A file that exists but is not valid JSON is reported as corrupt instead of being
overwritten.

**`Add`.**
1. Hash the raw text. If an entry with that hash exists, return it with `added=false`. This way
   pressing "Check Inbox" twice does not save each message twice.
2. Parse with `message.Parse`.
3. Fill the entry, take `NextID`, increment it, append. Nothing is written to disk yet; call
   `Save` after a batch.

**`Save`.**
1. `MkdirAll` creates `data/` if needed.
2. `MarshalIndent` produces readable JSON.
3. Write to `mailbox.json.tmp`, then `os.Rename` over the real file. Rename is atomic on the
   same filesystem, so a crash leaves either the old file or the new one, never half of each.

**`List`.** Returns a copy (`slices.Clone`) so a caller changing the slice cannot change the
mailbox.

**`Get`.** Linear search by local ID. For a personal mailbox of a few hundred messages
this costs almost nothing.

---

## Tests · `Tests/`

All tests are black-box: they import `email-client/pkg/...` and use only exported names.

| File | What it proves |
|---|---|
| `config/config_test.go` | Valid file loads with defaults; `EMAIL_PASSWORD` wins over the file; missing server, bad port, bad `from`, unknown key, and non-JSON all fail with a `config:` error. |
| `logger/logger_test.go` | Password passed as `password`, `PASS`, via `With`, and inside a group never appears; exactly 4 `[REDACTED]`; normal fields still print. **Block 1 "done when".** |
| `smtp/smtp_test.go` | A fake SMTP server on `127.0.0.1:0` (random free port) records every line. The test checks the exact command order, two `RCPT TO`s, the dot-stuffed `..starts with dot` line, the `.` terminator, and `QUIT`. Also: a 550 on RCPT is reported, connection refused is reported, and a silent server times out in about 200 ms instead of hanging. |
| `pop3/pop3_test.go` | A fake POP3 server: STAT count, LIST numbers and sizes, RETR un-stuffs `..leading dot` and converts CRLF, `-ERR` for a missing message is a `*pop3.Error`, wrong password is a `PASS` error, a socket cut mid-RETR says "transfer interrupted", and the Debug trace shows `PASS ****` but never the password. |
| `message/message_test.go` | Build → Parse round trip keeps From/To/Subject (with `é`)/Date/body; a newline in the subject cannot add a `Bcc:` header; an encoded-word subject from another client decodes; text with no headers is rejected. |
| `mailbox/mailbox_test.go` | Save → open a fresh `Mailbox` on the same file (a restart) → entry 2 is intact; the next new message gets ID 3 even though its POP3 number is 1; the same message added twice is stored once; a corrupt file is an error. **Block 4 "done when".** |

The fake servers prove the protocol handling. The plan's Block 2/3 "done when" checks need a
real server: after GreenMail is running, send a message with `smtp.Send` and read it back with
`pop3` to confirm.
