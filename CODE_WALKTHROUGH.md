# Code Walkthrough — Blocks 0–10 and added features

This file explains the code block by block, so the source itself can stay light on comments.
Read it next to the source files. Headings follow the file order.

```
NetworkLab/
├── Client/                 module email-client (the program)
│   ├── config.json
│   ├── cmd/cli/            the terminal menu (Block 5)
│   ├── cmd/serve/          the HTTP API process (Block 6)
│   └── pkg/
│       ├── account/        send / refresh / fetch for the one account
│       ├── config/         load + validate config.json
│       ├── logger/         slog with password redaction
│       ├── smtp/           SMTP client over a raw TCP socket
│       ├── pop3/           POP3 client over a raw TCP socket
│       ├── message/        build outgoing / parse incoming mail text
│       ├── mailbox/        JSON file of downloaded messages
│       ├── mailerr/        sorts errors into failure kinds (Block 9)
│       └── server/         HTTP JSON handlers (Block 6)
├── Tests/                  module email-client-tests (all tests + section5.sh)
├── Results/                §5 evidence: transcripts, log, timing, screenshots (Block 10)
└── WebUI/                  Vite + Tailwind web UI (Blocks 7–8)
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
- GreenMail runs with two fixed accounts, `alice@localhost` / `alice` and `bob@localhost` /
  `bob`, and checks passwords, so a wrong password is rejected (needed for the error-case tests).
- `from` is the address used in `MAIL FROM` and in the `From:` header.
- `password` can be left out of the file and supplied with `EMAIL_PASSWORD=... ` instead
  (see the config package).

**Handshake by hand (done):**

```
$ nc -c localhost 3025
220 /172.17.0.2 GreenMail SMTP Service v2.1.14 ready
EHLO me
250-/172.17.0.2
250-AUTH PLAIN LOGIN XOAUTH2
250 SMTPUTF8
QUIT
221 /172.17.0.2 Service closing transmission channel

$ nc -c localhost 3110
+OK POP3 GreenMail Server v2.1.14 ready
USER alice@localhost        -> +OK
PASS alice                  -> +OK        (PASS wrong -> -ERR Authentication failed ... Invalid password)
STAT                        -> +OK 0 0
QUIT                        -> +OK bye see you soon
```

The `250-` lines are a multi-line reply: `-` after the code means "more lines follow", a space
means "last line". `textproto.ReadResponse` in the SMTP client collects all of them.

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

**`ErrUnexpectedResponse`.** A sentinel error wrapped (`%w`) into every "this reply makes no
sense" case: a status that is neither `+OK` nor `-ERR`, or STAT/LIST numbers that do not parse.
`errors.Is(err, pop3.ErrUnexpectedResponse)` finds it however deeply it is wrapped. Block 9 uses
it to tell this case apart from the others.

**`Error` type.** Represents a `-ERR` answer. Having a type lets callers tell "server said no"
(wrong password, no such message) apart from network failures, using `errors.As`.

**`MsgInfo`.** One LIST line: message number and size in bytes.

**`Dial`.** Same TCP connect as SMTP, then reads the greeting with `reply("greeting")`, which
must be `+OK`.

**`reply`.** Extends the deadline, reads one line (`ReadLine` strips `\r\n`), logs it, then
`strings.Cut(line, " ")` splits it into status and text:
- `+OK` → return the text (e.g. `"2 320"` for STAT)
- `-ERR` → return `*Error`
- anything else → an error wrapping `ErrUnexpectedResponse`. This covers talking to a non-POP3
  server.

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

**`ParseRecipients`.** Turns the "To" text the user typed into a list of bare addresses.
It splits on commas, skips empty pieces (so a trailing comma is fine), and runs each piece
through `mail.ParseAddress`. That accepts both `bob@x` and `Bob <bob@x>` and keeps only the
`bob@x` part for `RCPT TO`. Anything it cannot parse, or an empty list, is an error. This is the
"invalid user input" check for recipients. Its errors are made with `mailerr.Input` (Block 9),
so both frontends know they are the user's mistake and not a server problem.

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

`mu sync.RWMutex` makes it safe for the HTTP server, which runs each request on its own
goroutine. `Add` and `Save` take the write lock (`Lock`), so only one runs at a time and nobody
reads meanwhile. `List` and `Get` take the read lock (`RLock`), so any number of reads can run
together. `defer mu.Unlock()` releases the lock on every return path. The CLI has a single
goroutine and pays almost nothing for the locks.

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

## Block 5a · `pkg/account/account.go`

The CLI now and the HTTP API in Block 6 both need "send", "download the inbox", "read one
message" and "list local messages". Those steps live here once, so each frontend only handles
input and output.

**`Account` struct.** Holds the config, the logger and the open local mailbox. `popMu` lets
only one POP3 session run at a time. RFC 1939 says a server locks the maildrop while a session
is open, so a second parallel login (two browser tabs clicking refresh) would just get `-ERR`.
Sending is not under this lock, so mail can go out while the inbox downloads.

**`InboxItem`.** A message as the server currently numbers it (`Num`) together with the saved
local copy (`mailbox.Entry`, embedded). The Inbox table shows `Num`, and "Read Message" asks
for it.

**`Open`.** Opens the mailbox file named in the config.

**`Send`.** `message.Build` creates the text, then `smtp.Send` runs the SMTP session.

**`session`.** Every POP3 action follows the same shape: connect, USER/PASS, do something,
QUIT. `session` takes `popMu`, does the connect, login and QUIT, and runs the middle part as the
function `fn`.
`defer c.Close()` releases the socket on every error path. QUIT is sent only on success,
because after a failure the connection may be in an unknown state.

**`Refresh`.** One POP3 session: LIST, then RETR for every message, adding each to the
mailbox. `added` counts the ones that were not already saved (the mailbox's duplicate check).
After the session, the mailbox is saved if anything was added, *even if a later RETR failed*,
so messages already downloaded are not thrown away. A save error is reported only if
nothing else failed first.

**`Fetch`.** One POP3 session with a single RETR. This is the spec's "retrieve a selected
message": "Read Message" goes back to the server for that one message instead of using the copy
from Check Inbox. The result is saved if it is new.

**`Local` / `LocalGet`.** Read-only access to saved messages, no network.

---

## Block 5b · `cmd/cli/main.go`

**`menu` const.** The five menu items, worded as in the spec.

**`main` / `run`.** `run` returns an error instead of exiting, so `main` is the only place that
prints `Error: ...` and exits with code 1. Startup order:
1. Flags: `-config` (default `config.json` in the current folder) and `-debug`.
2. `config.Load`. A missing password is reported here with how to fix it, before anything
   connects.
3. `-debug` switches the log level to Debug, which adds every `C:` / `S:` protocol line to the
   log. Without it the log has only the events (connected, sent, retrieved, errors).
4. `logger.Open` on `log_path`, closed when `run` returns.
5. `account.Open`, then the menu loop.

**`cli` struct.** The input scanner, the output writer, the account and the logger.

**`prompt`.** Prints a label, reads one line, trims spaces. `ok=false` means stdin ended
(Ctrl-D or the end of a piped file). Every caller then returns quietly, so the program exits
cleanly instead of looping on empty input.

**`loop`.** Shows the menu, reads a choice and calls the matching method. Every method returns
an error, and the loop hands it to `report`, then shows the menu again. That is why bad input or
a server failure never ends the program: the error is handled in this one place.

**`report`.** Runs the error through `mailerr.Explain`. It prints the plain-language headline,
plus a `detail:` line with the technical chain when there is one. It logs both together with
the error kind. Example:

```
Error: Login failed: wrong username or password.
  detail: pop3: PASS: server replied -ERR Authentication failed: ... Invalid password
```

The bad-input checks in this file (`invalid choice`, `not a message number`, `not an ID`,
`no saved message with ID`) use `mailerr.Input`, so they print with no detail line.

**`checkInbox`.** Calls `Refresh`, prints "N message(s) on server, M new saved locally", then
a table. `tabwriter` lines up columns by padding with spaces. Columns: POP3 number, sender,
subject, date.

**`readMessage`.** Asks for a number, rejects non-numbers and numbers below 1, calls `Fetch`
and prints the message. A number the server does not have comes back as POP3 `-ERR` and is
printed as an error.

**`compose`.** Asks for recipients (checked with `ParseRecipients` before anything connects)
and the subject. Then it reads body lines until a line with only `.`, the same end marker SMTP
uses. Lines starting with `.` are still allowed, because the SMTP DotWriter stuffs them. It
measures how long `Send` takes and prints it, e.g. `Sent to alice@localhost in 39 ms` (data for
the performance table).

**`viewLocal`.** Prints all saved messages with their local IDs, no network. Then it asks for
an ID to open. Pressing Enter goes back to the menu.

**`show`.** Prints headers and body of one message.

**`cut`.** Shortens long sender/subject text for the table. It counts in runes (characters), not
bytes, so a multi-byte character like `é` is never cut in half.

**`date`.** `unknown` for a missing date, otherwise local time as `2006-01-02 15:04`. That is
Go's layout syntax: you write the reference time in the format you want.

**Running it:**

```bash
cd Client
go run ./cmd/cli            # or: go build -o emailcli ./cmd/cli && ./emailcli
go run ./cmd/cli -debug     # also logs every protocol line
```

`data/` and `logs/` are created relative to the folder you run it from.

---

## Block 6a · `pkg/server/server.go`

The JSON API the web UI talks to. The handlers only translate HTTP to `Account` calls and back.

> **Updated for sign-in** (see "Added features" near the end). The server is now built with
> `server.New(cfg, log, webDir)`. Every mail route runs only for a signed-in user and gets that
> user's own `Account`. The table below lists the mail routes; the sign-in and picture routes
> are in the added-features chapter.

**`summary` / `full`.** The JSON shapes sent to the browser. They are separate from
`mailbox.Entry` on purpose: the API never exposes `hash` or `raw`, and changing the storage
format cannot silently change the API. `num` has `omitempty` so it disappears where it does not
apply.

**Routes.** Go 1.22+ `ServeMux` patterns include the method:
`"POST /api/send"` only matches POST, so a GET on it gets **405 Method Not Allowed** automatically.
`{num}` is a path wildcard read with `r.PathValue("num")`.

| Route | Does | Network |
|---|---|---|
| `GET /api/health` | Is each server sending its greeting? | SMTP connect + QUIT, POP3 connect + QUIT (no login) |
| `GET /api/inbox` | `Refresh`, returns `[{num, id, from, subject, date}]` | POP3 LIST + RETR all |
| `POST /api/refresh` | `Refresh`, returns `{on_server, added, ms}` | same |
| `GET /api/messages/{num}` | `Fetch(num)`, returns `{id, from, to, subject, date, body}` | POP3 RETR one |
| `POST /api/send` | `{to:[], subject, body}` → `Send`, returns `{sent_to, ms}` | full SMTP session |
| `GET /api/local` | every saved message with body | none |

`{num}` is the POP3 number from the inbox list, like "Read Message" in the CLI. The plan
wrote `:id`, but reading by POP3 number is what makes the server do a real RETR.

**Static files.** With `-web <folder>`, every non-API path is served from that
folder by `http.FileServer`. That is how the built web UI will be served (Block 7). `go:embed`
from the plan cannot be used: it only reaches files inside the Go module, and `WebUI/` is
outside `Client/`.

**`writeJSON`.** Sets `Content-Type: application/json`, writes the status code, encodes the
value.

**`fail` and `statusFor`.** Every handler error goes through `fail`. It runs `mailerr.Explain`,
logs the full error, and answers `{"error": headline, "kind": ..., "detail": ...}` (`detail`
only when there is one). Status codes:
- `400` invalid input: the request is wrong
- `504` timeout: Gateway Timeout, the server behind the API was too slow
- `500` unknown: a bug or a local problem, such as a disk error
- `502` everything else (unreachable, auth failed, rejected, unexpected, interrupted): Bad
  Gateway, meaning the API works but the mail server behind it failed.

The web UI can show `error` directly and branch on `kind`.

**`status` / `health`.** Each server's entry includes its `addr` (`localhost:3025`) so the web
UI can show which socket it is talking about. Each check is a small function literal called right away (`func()
error { ... }()`). It dials, which reads the greeting, and then sends QUIT. `status` turns the
result into `{"status":"up"}` or `{"status":"down","kind":...,"error":...}`. The health check
uses a fixed 3 s timeout, so the UI's status light answers quickly even when the configured
timeout is long.

**`message`.** `strconv.Atoi` on the path value. A non-number or a number below 1 is a 400
before any connection opens.

**`send`.**
- `http.MaxBytesReader` caps the body at 1 MB, so a huge upload cannot exhaust memory.
- `DisallowUnknownFields` turns a typo like `"too"` into a 400 instead of sending to nobody.
- Recipients go through the same `ParseRecipients` as the CLI. It joins the list with commas, so
  `["a@x", "b@x"]` and `["a@x, b@x"]` both work.
- It times the SMTP session and returns `ms`.

**`local`.** Converts every entry to `full`. No network.

---

## Block 6b · `cmd/serve/main.go`

Same startup as the CLI (flags, config, log), then an HTTP server. Since sign-in was added it
no longer needs a password in `config.json` and no longer opens an account at startup: each
person who signs in gets their own. `config.json` still supplies the server address, ports,
timeout and data folder.

**Flags.** `-config`, `-debug`, `-web` as above, and `-addr` (default `127.0.0.1:8081`). It
listens on localhost only by default. Sign-in protects the mail routes, but the session
cookie is sent over plain HTTP, which is fine on one machine and not across a network. The port is 8081 rather than the plan's 8080 because another program on this
machine already uses 8080.

**Listen, then announce.** `net.Listen` binds the port first. Only after that succeeds does it
print "API on ...". `srv.Serve(ln)` then serves on that socket. If the port is taken, the only
output is `Error: listen tcp 127.0.0.1:8081: bind: address already in use`. Calling
`ListenAndServe` directly would print the "API on" line and then fail, which misleads.

**`http.Server`.** `ReadHeaderTimeout: 5s` drops clients that open a connection and never
finish sending headers, so a slow client cannot hold connections open forever.

**Graceful stop.** `signal.NotifyContext` cancels `ctx` on Ctrl-C. The goroutine waiting on
`ctx.Done()` calls `srv.Shutdown`, which stops accepting new requests and gives running ones
up to 5 s to finish. `Serve` then returns `http.ErrServerClosed`, which is the normal
way out, not an error.

**Running it:**

```bash
cd Client
go run ./cmd/serve                         # API on http://127.0.0.1:8081
go run ./cmd/serve -web ../WebUI/dist      # also serve the built web UI (after Block 7)

curl -s localhost:8081/api/health
curl -s -X POST localhost:8081/api/send -d '{"to":["alice@localhost"],"subject":"hi","body":"hello"}'
curl -s localhost:8081/api/inbox
curl -s localhost:8081/api/messages/1
```

---

## Block 9 · `pkg/mailerr/mailerr.go`

The spec lists failures the client must survive. Every layer reports them differently:
`net` gives `*net.OpError`, `textproto` gives `*textproto.Error`, our POP3 code gives
`*pop3.Error`. This package maps all of them to one `Kind` so the CLI and the API show the same
message.

**`Kind` constants.** String-typed, so they go into JSON and logs as readable words
(`"auth_failed"`).

| Kind | Spec failure | Detected by |
|---|---|---|
| `invalid_input` | invalid user input | `*InputError` |
| `auth_failed` | invalid username/password | `*pop3.Error` whose command was USER or PASS |
| `rejected` | unexpected server response (a refusal) | any other `*pop3.Error` (-ERR) or `*textproto.Error` (SMTP 4xx/5xx, e.g. `550` on RCPT) |
| `timeout` | timeout | a `net.Error` whose `Timeout()` is true (socket deadline passed) |
| `unreachable` | connection failure | `ECONNREFUSED`, host/network unreachable, or `*net.DNSError` (bad hostname) |
| `interrupted` | interrupted transfer | `io.EOF`, `io.ErrUnexpectedEOF`, `ECONNRESET`, `EPIPE`: the other side closed or reset the socket |
| `unexpected_response` | unexpected server response (garbage) | `pop3.ErrUnexpectedResponse` or `textproto.ProtocolError` (a reply that is not `NNN text`) |
| `unknown` | anything else | fallback, e.g. disk errors |

**`InputError` / `Input`.** A distinct type for "the user typed something wrong". `Input` works
like `fmt.Errorf`. Its message is written for the user already, so it is shown without changes.

**`Classify`.** One `switch` with `errors.As` / `errors.Is`. Both look through every `%w`
wrapper, so `"smtp: connect localhost:3025: dial tcp ...: connection refused"` is still found to
contain `ECONNREFUSED`. **Order matters**:
- auth is checked before rejected, because a wrong password is also a `-ERR`;
- rejected and timeout come before unreachable and interrupted, because their types are more
  specific.

**`headlines` / `Explain`.** One plain sentence per network kind, plus the original error as
`detail` for the log and for debugging. For input and unknown errors the message itself is the
headline and there is no detail.

---

## Block 7 · `WebUI/`

Plain JavaScript modules, no framework. Vite serves them during development and bundles them
for production. Tailwind generates the CSS classes. Node.js is only the build tool; the browser
talks to the Go API and nothing else.

```
WebUI/
├── package.json      scripts + dependencies
├── vite.config.js    Tailwind plugin, /api proxy
├── index.html        the page shell (stripe, sidebar, main, status bar)
└── src/
    ├── main.js       entry: route table, start router + health check
    ├── style.css     theme tokens, Tailwind setup, two components
    ├── dom.js        h(): build elements safely
    ├── api.js        fetch wrapper for /api/*
    ├── router.js     hash router
    ├── health.js     status bar (SMTP / POP3 up or down)
    ├── ui.js         shared pieces: list, message view, error box, buttons, dates
    └── views/        one file per screen (Block 8)
        ├── inbox.js  ├── message.js  ├── compose.js  ├── saved.js  └── not-found.js
```

**Run it:**

```bash
cd Client && go run ./cmd/serve           # terminal 1: API on :8081
cd WebUI  && npm install && npm run dev   # terminal 2: http://localhost:5173

# production / demo: one process, no Vite
cd WebUI  && npm run build                # writes WebUI/dist/
cd Client && go run ./cmd/serve -web ../WebUI/dist   # http://localhost:8081
```

### `package.json`

`"type": "module"` makes `.js` files ES modules (`import`/`export`). Scripts: `dev` starts Vite's
dev server with live reload, `build` writes optimized files to `dist/`, `preview` serves `dist/`
for a quick check. Dev dependencies are build tools only (`vite`, `tailwindcss`,
`@tailwindcss/vite`). The two `@fontsource` packages contain the IBM Plex font files. They get
bundled into `dist/`, so the demo does not need internet access.

### `vite.config.js`

- `tailwindcss()` lets Vite process the `@import "tailwindcss"` in `style.css`. This is Tailwind
  v4: configuration lives in CSS, so there is no `tailwind.config.js`.
- `proxy`: in development the page comes from `:5173` but the API is on `:8081`. Vite forwards
  every `/api/...` request to the Go server, so the browser sees a single origin and no CORS
  setup is needed. `API_URL=http://127.0.0.1:9000 npm run dev` points it somewhere else.

### `index.html`

The fixed page shell. JavaScript fills `#login`, `#profile`, `#view` and `#status`.

> **Updated for sign-in and the theme toggle.** The body now holds two screens, `#login` and
> `#app`, both starting `hidden`. `main.js` shows one of them. The airmail stripe moved inside
> `#app`, because the login card carries its own. An inline script in `<head>` paints the
> saved theme before anything is drawn. Details in "Added features".
- `<html data-theme="dark">`: the attribute every color depends on (see `style.css`).
- `<script type="module" src="/src/main.js">`: Vite rewrites this to the bundled file on build.
  Module scripts run after the HTML is parsed, so the elements already exist.
- `.airmail`: the striped top border, `aria-hidden` because it is decoration.
- Layout: `body` is a vertical flex column (stripe, content, status bar), and `flex-1` lets the
  content take the remaining height. The content is a CSS grid:
  - on phones: two rows, `grid-rows-[auto_1fr]`. The nav row is only as tall as it needs; the
    view gets the rest.
  - from `md` (768 px) up: two columns, `md:grid-cols-[13rem_1fr]`, sidebar plus view.
  Without `grid-rows-[auto_1fr]` the phone layout splits the height 50/50 and leaves a tall,
  empty header.
- `nav` links carry `data-nav="inbox"` etc. The router uses it to mark the current page.
- `<main id="view" tabindex="-1">`: `tabindex="-1"` lets JavaScript move keyboard focus there
  after navigation without adding it to the Tab order.
- `#status` is a `<button>`, because clicking it re-checks the connection. A button is
  reachable with the keyboard and announced as clickable.

### `src/style.css`

1. `@import "tailwindcss"` brings in Tailwind. The font imports pull in Plex Sans 400/600 and
   Plex Mono 400.
2. **Theme tokens.** `:root[data-theme="dark"] { --bg: ...; --fg: ...; }` defines every color
   once, as a CSS variable. `color-scheme: dark` tells the browser to draw its own parts
   (scrollbars, form controls) dark as well.
3. **`@theme inline`** turns each variable into Tailwind colors: `--color-bg: var(--bg)` creates
   `bg-bg`, `text-bg`, `border-bg`, and so on. `inline` means the class uses `var(--bg)`
   directly instead of copying the value, so a class always follows the active theme.
   `--font-sans` and `--font-mono` replace Tailwind's default fonts.
4. **`.airmail`.** A `repeating-linear-gradient` at −45°: 10 px red, 6 px gap, 10 px blue, 6 px
   gap, repeated. That is the airmail envelope edge, drawn with no image.
5. **`.nav-link`.** `@apply` bundles the Tailwind classes shared by the three links. The
   `[aria-current="page"]` rule highlights whichever link the router marked as current.
6. **`:focus-visible`.** An accent outline whenever the keyboard focuses something. Mouse clicks
   do not trigger it.

**Light mode** is now in place (see "Added features"): one more block of the same variables
under `:root[data-theme="light"]`. No HTML or JavaScript class changed. Every `bg-bg` /
`text-fg` / `border-line` follows whichever block is active.

### `src/dom.js`, `h()`

`h('a', { href: '#/inbox', class: 'nav-link' }, 'Inbox')` creates `<a href="#/inbox"
class="nav-link">Inbox</a>`.
- props starting with `on` become event listeners (`onClick` → `click`).
- `class` sets `className`; everything else is `setAttribute`.
- children are flattened (arrays allowed), and `null`/`false` are skipped, so
  `cond && h(...)` works.
- `el.append("text")` creates a **text node**. Email content will be shown through this, so a
  message containing `<script>` is displayed as those characters and never runs. Using
  `innerHTML` with message data would allow exactly that injection.

### `src/api.js`

- `ApiError` extends `Error` with the Go server's `kind` and `detail` fields (Block 9), so a
  screen can show `err.message` and branch on `err.kind`.
- `api(path, {method, body})`: prefixes `/api`, sends JSON when there is a body, and returns
  the parsed reply.
- Two ways the API can be missing, both shown as "The client API is not running...":
  - `fetch` itself throws (nothing is listening, e.g. in production with Go stopped);
  - the reply is an error with no JSON body (the Vite proxy answers with an empty error when Go
    is stopped).
- Otherwise a non-2xx status throws `ApiError(data.error, data.kind, data.detail)`.
- Since sign-in: a `File`/`Blob` body is sent as is (picture upload), and a 401 from any route
  except `/login` and `/me` fires the `mail:signedout` event. See "Added features".

### `src/router.js`

Routes live after `#` in the URL (`#/message/3`). Changing the hash never reloads the page or
asks the server for anything, so the Go file server only ever has to serve `/`.
- `render` reads `location.hash` and defaults to `/inbox`.
- `routes.find(...)` tests each route's regex and keeps the match. `(\d+)` capture groups
  become the view's arguments (`match.slice(1)` → `['3']`).
- It sets `aria-current="page"` on the nav link whose `data-nav` equals the route's `nav`, and
  removes it from the others. That drives the CSS highlight and tells screen readers which page
  is current. A message page keeps "Inbox" highlighted.
- **A fresh `<div>` per visit.** Block 8 views will `await` the API. If you click away before a
  slow request returns, the old view writes into its own `<div>`, which is no longer on the
  page, so it cannot overwrite the new screen. No cancel logic is needed.
- Focus moves to `#view` after a click-driven change (`hashchange`), not on first load. A
  keyboard user then continues from the new content instead of the nav.
- `startRouter` returns `render`. After someone signs in, `main.js` calls it to redraw the
  current screen with the new account's data, without adding a second `hashchange` listener.

### `src/health.js`

- `socket(label, s)` renders `SMTP localhost:3025 ● up`: green `text-ok` for up, red `text-bad`
  for down, and the server's error text as a hover tooltip.
- `check()` calls `/api/health` and replaces the status bar contents. On failure it shows the
  error in red. (It used to fill in the account name too; the profile block does that now,
  from `/api/me`.) Health needs no sign-in, so the bar works on the login screen as well.
- **When it runs:** on page load, when the tab becomes visible again (`visibilitychange`),
  when the bar is clicked, and when a screen hits a network error (the `mail:recheck` event from
  `errorBox`, see below). Not on a timer: each check opens real SMTP and POP3 connections and
  writes "Connected to ..." to the client log, and polling every few seconds would fill the log
  you hand in.
- The "checked 3:45 PM" time has `hidden sm:inline`, so it only appears on screens 640 px and
  wider. On a phone it would wrap onto its own line.

### `src/main.js`

Imports the CSS (Vite injects it), declares the route table (regex → view, plus which nav item
to highlight), and starts the router and the health check. Since sign-in it also decides
between the login screen and the app; see "Added features".

| Hash | Screen | Nav |
|---|---|---|
| `#/inbox` | server inbox | Inbox |
| `#/message/3` | message 3 fetched with POP3 RETR | Inbox |
| `#/compose` | send form | Compose |
| `#/saved` | messages saved on this computer | Saved |
| `#/saved/5` | saved message with local ID 5 | Saved |

**Block 7 "done when":** with `go run ./cmd/serve` plus `npm run dev`, the page
loads styled and the status bar shows `SMTP localhost:3025 ● up  POP3 localhost:3110 ● up`. With
the Go server stopped, the bar shows the red "not running" hint. The built `dist/` served by
`serve -web` works too. Phone width (390 px) shows the nav as a single top row.

---

## Block 8 · the screens

Every screen is a function `(el, ...params)` that the router calls with a fresh container. Each
one follows the same pattern: put a loading line in place, `await` the API, then **replace**
the loading line with the result or with an error box. Nothing is cached in the browser. The Go
server and `mailbox.json` hold all state, so a page reload always shows the truth.

### `src/ui.js`: shared pieces

**`setTitle` / `header(title, ...actions)`.** Sets the tab title (`Inbox · Mail`) and returns
the heading row. Actions (a count, a button) sit on the right. The row uses `flex-wrap`, so on a
phone they drop under the title instead of squeezing it.

**`note`, `link`, `backLink`.** A muted paragraph, an accent link, and the `← Inbox` link at the
top of a message.

**`button(label, onClick, {primary, type})`.** Primary is filled with the accent color; the
default has an outline. `disabled:opacity-50 disabled:cursor-wait` shows that a request is
running. `...(onClick && { onClick })` adds the listener only when one is given. The submit
button in Compose has none, because the form's `submit` event handles it.

**`loading(text)`.** A muted line with `role="status"`, so screen readers announce "Downloading
from the POP3 server…" without moving focus.

**`errorBox(err, retry)`.** The UI half of Block 9:
- the headline (`err.message`, from the Go server's `error` field) in red;
- `err.detail` inside a closed `<details>`. The technical chain is one click away, but it does
  not crowd the message;
- a "Try again" button when the screen can retry;
- `role="alert"`, so a screen reader reads it out at once;
- for network failures (any `kind` except `invalid_input`) it fires
  `document.dispatchEvent(new Event('mail:recheck'))`. `health.js` listens and re-checks, so the
  status bar never keeps saying "up" after a request just failed to connect. The two modules
  share only the event name and do not import each other.

**`parseDate` / `shortDate` / `longDate`.** Go encodes a missing Date header as
`0001-01-01T00:00:00Z`. Anything before 1971 or unparsable counts as unknown (`—` in lists,
`unknown` in a message). In lists, today's mail shows the time (`04:04 PM`), this year's shows
`Oct 6`, older mail adds the year. All formatting uses the browser's locale.

**`messageList(items, href, label)`.** Used by Inbox and Saved. Each row is one `<a>`, so the
whole row is clickable, Tab-reachable, and opens with Enter. `label(m)` is the first column: the
POP3 number in Inbox, the local ID in Saved. The layout is a CSS grid that changes with screen
width:
- **desktop** (`md:`): four columns `3rem | 14rem | 1fr | auto`: number, sender, subject, date.
- **phone**: two columns. The number is hidden; sender and date on line 1; the subject has
  `col-span-2 row-start-2`, so it fills line 2. The grid places the date in the empty cell next
  to the sender automatically.
- `truncate` cuts long senders and subjects with `…` instead of wrapping.
- An empty subject shows an italic `(no subject)`.

**`messageView(m, ...footer)`.** Subject as the heading, a `<dl>` with From / To / Date, then the
body in a `<pre>`:
- `<pre>` keeps the line breaks and spacing exactly as sent. Plain-text mail often relies on
  them (signatures, quoted replies, ASCII tables), and the monospace font matches.
- `whitespace-pre-wrap break-words` still wraps long lines, so the page never scrolls sideways.
- All values go in through `h()` as text nodes. The browser test sent the subject
  `<script>alert(1)</script>` and a body containing `<img src=x onerror=alert(2)>`. Both showed
  up as plain characters and no script ran.

### `views/inbox.js`

- On open, and when **Check for new mail** is clicked, it calls `GET /api/inbox`. On the Go
  side that is one POP3 session: LIST, then RETR for each message, then saving new ones
  (Block 5a `Refresh`).
- The button is disabled while loading, so double-clicking cannot start a second session.
  `finally` turns it back on whether the request succeeded or failed.
- `performance.now()` measures the round trip and the header shows `31 on server · 126 ms`.
  This time includes HTTP and the whole POP3 session, which is useful for the performance
  section of the report.
- `items.toReversed()`: POP3 numbers count up from the oldest, so reversing puts the newest
  first. `toReversed` returns a copy and leaves the original array as it is.
- Rows link to `#/message/<num>`.
- Empty inbox: a note with a link to Compose. Error: `errorBox` with retry, e.g. "Login failed:
  wrong username or password."

### `views/message.js`

- `GET /api/messages/{num}` makes the server do a fresh POP3 RETR for just this message. This is
  the spec's "select one message and view its content", done over the network and not from the
  cached copy.
- The footer says which POP3 number it was and links to the saved copy (`#/saved/<id>`). That
  makes the difference between the server number and the local ID visible.
- A number the server does not have (`#/message/9999`) shows "The mail server refused the
  request." with the server's `-ERR no such message` under Technical detail.

### `views/compose.js`

- **`labelled(id, text, control)`.** A `<label for=id>` over its field. Clicking the label
  focuses the field, and screen readers read the label when the field is focused.
- Fields: **To** (`required`, so the browser blocks an empty submit), **Subject**, **Message**
  (a monospace `<textarea>`, so what you type looks like what the receiver sees).
- The `<form>` `submit` event fires for both the button and Enter in a field. `preventDefault()`
  stops the browser's own page-reload submit.
- **To** is split on commas, trimmed and stripped of blanks, then sent as an array. The Go
  server validates every address again (`ParseRecipients`). The browser is never trusted to
  validate.
- While sending, the button says "Sending…" and is disabled.
- **Success:** the form is cleared, a green line says `Sent to alice@localhost, bob@localhost.
  SMTP session took 20 ms.` with a link to the Inbox, and focus returns to **To** for the next
  message. `ms` here is measured by the Go server around the SMTP session itself.
- **Failure:** `errorBox`, and the form keeps what you typed. For example `nope` gives `invalid
  recipient "nope"`.
- `result` has `aria-live="polite"`, so the success or error line is announced.

### `views/saved.js`

- **`saved`.** `GET /api/local` returns everything in `data/mailbox.json`. No mail server is
  involved, so this screen works even when GreenMail is stopped. That is the spec's "view again
  after restarting" requirement. Newest first, labelled with local IDs, linked to
  `#/saved/<id>`. Empty: a note explaining that opening the Inbox fills it.
- **`savedMessage`.** Loads the same list and picks the entry with `id === Number(id)`. The hash
  gives a string, hence `Number`. Not found gives a plain `Error` in the error box. A plain
  `Error` has no `kind`, so it does not trigger a status re-check. The footer says the copy was
  read from this computer.

### `views/not-found.js`

Any hash that matches no route, with a link back to the Inbox.

### `index.html`, favicon

`<link rel="icon" href="data:image/svg+xml,...">` is a tiny envelope drawn as inline SVG. Without
it the browser requests `/favicon.ico` on every load and logs a 404.

**Block 8 "done when", checked in a real browser** (Brave driven by Playwright against
`serve -web dist`):
- Compose → send to two recipients → green "Sent" line (20 ms).
- Bad recipient → error box, form kept.
- Inbox lists 31 messages (126 ms) → click the new one → body shown as written, dot line and
  HTML as plain text → "Saved as #31" → saved copy opens.
- Saved lists 31.
- Phone width (390 px): stacked rows, nav in a single top row.
- `#/message/9999` → "refused"; `#/saved/9999` → "No saved message".
- Wrong password → "Login failed" with retry. Mail server down → "Cannot reach the mail server"
  and the status bar turns red.
- Keyboard: Tab reaches the nav with a visible outline.
- No console errors except the intentional 400/502 responses, and no script ever ran from message
  content.

---

## Block 10 · testing and timing

### `Tests/perf/perf_test.go`

Measures how long the protocol operations take. Run it with `PERF=1 go test ./perf -v`.

**Skip by default.** `if os.Getenv("PERF") == "" { t.Skip(...) }`. Each run sends 30 messages,
some of them 100 KB. In a normal `go test ./...` that would fill the inbox and slow every
later run, so it only runs when asked.

**bob's account.** The test overrides `Username`, `Password` and `From` with bob's, and uses a
temporary mailbox file. alice's inbox, which the demo uses, stays untouched.

**`body(n)`.** Realistic plain text: 72-character lines (71 characters plus `\n`), repeated
and cut to exactly `n` bytes. One long line would be unrealistic, because SMTP limits lines to
1000 characters.

**`timeIt`.** `time.Now()` before and `time.Since` after one call. An error fails the test, so a
broken run can never produce a table.

**`summarize`.** Converts the durations to milliseconds with one decimal, sorts them, and returns
min, median (middle of the sorted list), mean and max. The median ignores the occasional slow
outlier, such as the first connection. The mean shows it.

**Three measurements:**
1. **Send**, 10× each at 1 KB, 10 KB, 100 KB. Each call is `acct.Send`, a whole SMTP session.
2. **Retrieve one**. One `Refresh` finds the POP3 number of the first message of each size by its
   subject (`perf-<time> 10 KB #0`). Then `Fetch(num)` runs 10× per size: connect, USER, PASS,
   RETR, QUIT.
3. **Check inbox**: `Refresh` 5×. That is LIST plus RETR of every message, the same work as
   Check Inbox in the CLI and web UI.

**Output.** A Markdown table with the date, Go version and platform, logged with `t.Log` and
written to `Results/timing.md`.

### `Tests/section5.sh`

One command that produces every piece of §5 evidence in `Results/`:

```bash
Tests/section5.sh --reset            # restart GreenMail (empty inbox), then everything
Tests/section5.sh --no-go-tests      # skip the Go suite (it sends extra mail to alice)
```

**Setup.** `set -uo pipefail`: unset variables and failures inside pipes count as errors. `-e` is
left off on purpose: the error-case runs are supposed to fail, and the script must continue.
`wait_ports` polls ports 3025 and 3110 with `nc -z` (connect only, send nothing) for up to 30 s
after a restart. A fresh `Results/run/` gets a copy of `config.json` and a freshly built CLI.
`PASSWORD` is read out of the config with `sed`, so the script never hard-codes it.

**`session TITLE INPUT [VAR=value ...] [-- flags]`.** Runs the CLI once with `INPUT` piped to
stdin and `-debug` on, and prints the typed input above the program output. A piped stdin is not
echoed, so without this the transcript would show prompts with no answers. Environment variables
(like `EMAIL_PASSWORD=...`) go before `--` and CLI flags (like `-config ...`) after it.
`${envs[@]+"${envs[@]}"}` expands to nothing when the list is empty; a plain `"${envs[@]}"`
would fail under `set -u` in older bash versions. `$SECONDS` (whole seconds since the shell
started) gives the rough duration, which shows the 2 s timeout.

**Steps:**
1. **Handshakes** with `printf ... | nc`. The printed command shows `PASS ****` while the real
   password goes to the socket.
2. **Send 3, check, read 2.** One CLI process: three Compose runs (the second to two recipients,
   with a body line starting with `.`), Check Inbox, Read 2, Read 3.
3. **Reopen.** A second process (a restart) runs View Local Messages and opens ID 3. Then
   `mailbox.json` is copied out.
4. **Error cases.**
   - `config-down.json` points both ports at 1, where nothing listens.
   - `config-timeout.json` points at 39999 with `timeout_seconds: 2`. A small Python listener
     there accepts connections and never writes, which is a server that hangs. It is stopped
     with `kill` + `wait` afterwards; `wait` collects the process quietly so bash prints no
     "Terminated" line.
   - Wrong password through `EMAIL_PASSWORD`.
   - Bad menu, number, recipient and ID input.
5. **Log check.** `grep -c` counts lines with the real password, the wrong password, the masked
   `PASS ****`, and each spec event.
6. **Timing.** Runs the perf test above.
7. **Go suite.** `go test -v`, filtered with `grep` down to the PASS/FAIL lines.

The CLI binary is deleted at the end; it does not belong in the submission.

### `Results/`

`Results/README.md` is the §5 checklist with the outcome and evidence file for each point, the
error-case table, the timing table and observations. The main observation: a 100 KB RETR is
about 3× slower than a 100 KB send because GreenMail writes one line per TCP segment (672
segments for 104 KB, measured with a raw socket), and the client reassembles them correctly.

---

## Added features · sign-in, profile picture, light/dark toggle

Three features on top of the spec. Two of them come from the spec's own bonus list:
- sign-in with any mail account, which is **multiple accounts**;
- the web UI, which is a **graphical interface**.

The CLI is unchanged and still uses `config.json`.

```
Client/pkg/server/
├── server.go     routes; mail handlers now receive the signed-in user
├── auth.go       sessions, login, logout, /api/me, the authed() guard
└── avatar.go     GET / PUT /api/avatar
Client/pkg/account/account.go   + SetPassword, Email
WebUI/src/
├── theme.js      the toggle, ported from ParkingRecog
├── login.js      the sign-in card
├── profile.js    round picture + menu in the sidebar
└── main.js       login screen ⇄ app
```

| Route | Signed in? | Does |
|---|---|---|
| `POST /api/login` `{email, password}` | no | checks the password with a real POP3 USER/PASS, sets the session cookie, returns `{email, has_avatar}` |
| `POST /api/logout` | no | deletes the session, clears the cookie, `204` |
| `GET /api/me` | yes | `{email, has_avatar}`; `401` when signed out |
| `GET /api/avatar` | yes | the picture bytes, or `404` |
| `PUT /api/avatar` (raw image body) | yes | saves the picture, returns `{email, has_avatar}` |
| `GET /api/health` | no | unchanged, minus the `username` field |
| all mail routes | yes | unchanged, for the signed-in account |

Data for each account lives in `Client/data/accounts/<email>/`: `mailbox.json` and `avatar`.

### `pkg/account/account.go` additions

**`SetPassword`.** After a re-login (the same person on a second browser) the existing
`Account` gets the latest password. Only POP3 sessions read the password, and they all run
under `popMu`, so taking `popMu` while writing is enough to keep the race detector quiet. SMTP
needs no password here: GreenMail accepts mail without SMTP AUTH.

**`Email`.** The address the account sends as (`cfg.From`).

### `pkg/server/server.go` changes

**`Server` struct.**
- `cfg` is now the *base* config: server address, ports, timeout, data folder.
- `sessions` maps cookie token → session, and `accounts` maps lowercased email → `*Account`.
  `mu` guards both maps.
- `Verify` is the function that checks a password. It is a field so tests can swap in one that
  accepts anything (needed when the mail server in the test is fake or down). The default is
  `pop3Login`.

**`New`.** Fills the maps, sets `Verify`, and registers the routes. Every mail route is wrapped
in `s.authed(...)`. `health`, `login` and `logout` are not, because they must work signed out.

**`ServeHTTP`.** Makes `*Server` itself an `http.Handler`, so `cmd/serve` and `httptest` can use
it directly.

**Mail handlers** (`inbox`, `refresh`, `message`, `send`, `local`) take a third parameter,
`u user`, and use `u.acct` in place of the single shared account. Their logic is unchanged.

### `pkg/server/auth.go`

**`session` / `user`.** A session is an account key plus an expiry time. A `user` is what a
handler receives: the key (lowercased email), the address as typed, and the `Account`.

**`pop3Login`.** The sign-in check: connect, `USER`, `PASS`, `QUIT`. The mail server is the only
place passwords live, so asking it is the honest test, and this app stores no password hashes of
its own. The POP3 client already logs `PASS ****`, never the password.

**`safeName`.** Turns an email into a folder name: lowercase, and every character other than
`a-z 0-9 . _ @ -` becomes `_`. A `/` can therefore never appear, so `../` tricks cannot leave
`data/accounts/`. The name always contains `@`, so it can never be exactly `..`.

**`userDir`.** `data/accounts/<safe name>/`, next to the CLI's `mailbox.json`.

**`accountFor`** (called with `mu` held). One `Account` per email. Two browsers signed in as
alice then share one mailbox file and one POP3 lock; two separate `Account`s would write the same
file without coordinating. On the first sign-in it copies the base config (`c := *s.cfg`, a
copy of the struct, not a pointer to it), sets username, password, From and mailbox path for
this user, and opens the account.

**`login`.**
1. Decode `{email, password}`, at most 4 KB, unknown fields rejected.
2. `mail.ParseAddress` must give back exactly the input. That accepts `alice@localhost` and
   rejects `alice`, `Alice <alice@x>` and empty input with "Enter an email address like
   alice@localhost." An empty password gets "Enter your password." Both are 400.
3. `s.Verify`. A wrong password (`AuthFailed`) is answered **401** with "Login failed: wrong
   username or password." The response never says whether the address exists. Any other failure
   (POP3 server down, timeout) goes through `fail` as before: 502 or 504 with its own message,
   so "server down" never reads as "wrong password".
4. `rand.Text()` gives a random 26-character token (128 bits, Go 1.24+). Guessing one is not
   feasible.
5. Under `mu`: get or create the account, store the session with `expires = now + 12h`.
6. The cookie:
   - `HttpOnly`: page JavaScript cannot read it, so even an injected script could not steal it.
   - `SameSite=Strict`: the browser does not send it on requests started from other sites, which
     blocks cross-site request forgery. Together with JSON-only bodies, no CSRF token is needed.
   - `MaxAge`: 12 h, matching the server-side expiry.
   - `Path=/`.
   - `Secure` is left off because the demo runs over plain `http://localhost`.
7. Logs `Signed in user=...` and answers like `/api/me`.

**`logout`.** Deletes the session if the cookie names one, logs `Signed out`, and sends a cookie
with `MaxAge: -1` so the browser deletes it. It answers `204` whether or not you were signed in;
signing out twice is not an error.

**`current`.** Cookie → session → account. An expired session is deleted when found, so old
sessions do not pile up in the map.

**`authed`.** Wraps a handler: no valid session means `401 {"error":"Sign in first.",
"kind":"unauthenticated"}`, and the handler never runs. One wrapper guards every mail route, so
none can be forgotten.

**`me`.** `{email, has_avatar}`, where `has_avatar` is whether the picture file exists.

### `pkg/server/avatar.go`

**`avatarTypes`.** PNG, JPEG, GIF, WebP. The type is found by `http.DetectContentType`, which
reads the file's first bytes (its "magic number"). It does not trust the request's
`Content-Type`, which anyone can set. SVG is deliberately not allowed: an SVG can contain
script.

**`avatar` (GET).** Reads the file and serves it with the detected type and `Cache-Control:
no-cache`, so a new picture shows up at once. No file → `404`.

**`setAvatar` (PUT).**
- `http.MaxBytesReader` stops reading at 2 MB. Going over gives `*http.MaxBytesError`, reported
  as "Pictures must be 2 MB or smaller."
- Then the type check: anything else gets "Choose a PNG, JPEG, GIF or WebP image."
- The file is written as `avatar.tmp` and renamed over `avatar` (atomic, like the mailbox), and
  the change is logged with its size.

### `cmd/serve/main.go` changes

The `no password` check and the startup `account.Open` are gone, and the handler is
`server.New(cfg, log, *webDir)`.

### `WebUI/index.html` changes

- **Theme boot script** (inline, in `<head>`, before the module script). It reads
  `localStorage.theme`; if that is not `light` or `dark`, it follows the OS
  (`prefers-color-scheme`). Then it writes `data-theme` and `color-scheme` on `<html>`. A plain
  inline script blocks rendering until it has run, so the first frame already has the right
  colours. A module script runs only after parsing, which would show a dark flash on a light
  machine. It duplicates a few lines of `theme.js` on purpose: a module cannot be inlined there.
- **Two screens.** `#login` (centred, for the sign-in card) and `#app` (stripe + sidebar + view),
  both `hidden` at first, so neither shows until `main.js` knows whether you are signed in.
- **`#profile`** replaces the old "Mail / address" lines in the sidebar.
- `main` gets `pb-20` on phones so the last list row can scroll clear of the floating theme
  button.

### `WebUI/src/style.css` additions

- **`:root[data-theme="light"]`.** The light palette. It is not the dark one inverted: `accent`,
  `ok` and `bad` are darker shades of the same hues, so they still read as text on white
  (`ok` went from `#5fc9a0` to `#1a7a57`). `--lift` is the drop shadow, lighter on light.
- **`.theme-fab`.** The round 46 px button, fixed bottom-left. `bottom: calc(3rem + ...)` keeps
  it above the status bar. `env(safe-area-inset-*)` keeps it clear of a phone's rounded corners.
  The colours come from the theme variables, and `:active` shrinks it slightly when pressed.
  `.rays` draws the sun's rays as strokes around a filled core, so sun and moon look equally
  heavy (from ParkingRecog).
- **`[data-theme-anim]` rule.** The fallback cross-fade (0.32 s on colours). `theme.js` sets the
  attribute only during a switch; a permanent transition would also slow every hover.
- **View-transition rules.** `::view-transition-old(root)` stays still and
  `::view-transition-new(root)` is revealed by `theme-wipe`, a `clip-path: circle()` growing from
  `--vt-x, --vt-y` to `--vt-r`. The browser's default cross-fade is switched off so the circle
  has a hard edge.
- **Reduced motion.** With `prefers-reduced-motion: reduce` the animations are removed; the
  colours still change.
- **`.menu-item`.** Full-width buttons in the profile menu.

### `WebUI/src/theme.js`

A port of ParkingRecog's `lib/theme.ts` + `ThemeToggle.tsx`, without React.

- **Three-state preference, two-state button.** Stored: `system`, `light` or `dark`. Shown:
  only `light` or `dark`. A browser that never pressed the button follows the OS, including when
  the OS switches at sunset (`lightQuery` `change` listener, acted on only while the preference
  is `system`). The first press stores an explicit choice, which wins over the OS from then on.
- **`stored()`.** Reads `localStorage` in `try`, because Safari private mode can throw.
- **`paint(theme)`.** Sets `data-theme` (what the CSS reads) and `color-scheme` (so scrollbars and
  form controls match).
- **`armFade()`.** Sets `data-theme-anim` for 380 ms (the 320 ms fade plus a margin). Pressing
  twice quickly restarts the timer instead of stacking timers.
- **`apply(update, origin)`.**
  1. Reduced motion → just `update()`.
  2. No `document.startViewTransition` (Firefox today) → the cross-fade.
  3. Otherwise the circle. The radius is the distance from the button to the *farthest* corner
     (`Math.hypot` of the larger horizontal and vertical gaps), so the circle exactly covers
     the screen at any window size. `startViewTransition(update)` captures the old frame, runs
     `update`, and animates to the new frame.

  The fade and the circle never run together. The fade would still be in progress when the
  browser captures the "new" frame, so the circle would reveal half-changed colours.
- **`mountThemeToggle()`.** Creates the button with `aria-label="Dark mode"`.
  - **`render()`.** The icon shows the theme you are **in** (moon when dark, sun when light), and
    `aria-pressed` says whether dark mode is on. That is the ARIA toggle pattern: the name says
    what the button controls and `aria-pressed` gives its state, so the icon and the screen
    reader agree.
  - **The click handler.** Flips the preference, saves it, and starts the switch from the
    button's **centre**. Keyboard presses (Enter/Space) report pointer position 0,0, which would
    start the circle in the corner.
  - Paint and icon change happen together inside `update`, so the revealed frame already shows
    the new icon.
  - The SVG icons are fixed strings set with `innerHTML`. That is safe because no user data goes
    into them; `h()` cannot make SVG elements, which need a different namespace.

### `WebUI/src/login.js`

The card is an envelope: an `.airmail` edge on top of a `bg-surface` card.
- **Fields.** `type="email"` and `autocomplete="username"` / `"current-password"`, so password
  managers fill them in. Both are `required`, so the browser blocks an empty submit.
- **`notice`** (green, `role="status"`) shows messages such as "You signed out." **`problem`**
  shows an error at once, e.g. the API is not running when the page loads.
- **`signIn`.** Posts `{email, password}`.
  - On success: `onSignedIn(me)`.
  - On failure: the shared `errorBox`, the password field is cleared and focused for the retry,
    and the email stays as typed.
  - The button says "Signing in…" and is disabled meanwhile.

### `WebUI/src/profile.js`

`mountProfile(el, me, {onSignOut})` builds the sidebar identity: the round picture, "Mail", and
the address.

- **`cleanup` / `AbortController`.** The menu closes on any click outside it, which needs a
  listener on `document`. Every sign-in mounts the profile again, so the previous mount's
  listeners are removed first with `cleanup.abort()`. Otherwise each sign-in would add one more.
- **`face`.** The 40 px round button, labelled "Account menu for <address>". `aria-expanded` and
  `aria-controls` tell screen readers it opens the menu.
- **`drawFace()`.**
  - With a picture: an `<img>` from `/api/avatar?v=<time>`. The changing `?v=` makes the browser
    fetch the new picture instead of reusing the old one.
  - Without one: the address's first letter on a raised circle.
- **The menu.** A small box under the picture with the address, **Change picture** and **Sign
  out**.
  - It is a plain disclosure (a button that shows a box of buttons) rather than an ARIA `menu`.
    That role would also require arrow-key navigation, and a half-built ARIA menu is worse for
    screen readers than plain buttons.
  - Escape closes it and puts focus back on the picture.
- **Change picture.** Opens the hidden `<input type="file">` (filtered to the four image types).
  1. Files over 2 MB are refused in the browser before uploading. The server checks again,
     because a browser check can be bypassed.
  2. Uploads with `api('/avatar', {method: 'PUT', body: file})`.
  3. Redraws the face.
  4. Errors show inside the menu.
- **Sign out.** Calls `onSignOut` from `main.js`.

### `WebUI/src/api.js` changes

- **Raw bodies.** `body instanceof Blob` (a `File` is a `Blob`) is sent unchanged, with no JSON
  header; the browser sets the type.
- **`mail:signedout`.** A 401 on any route except `/login` (where it means "wrong password") and
  `/me` (where it means "not signed in yet") means the session ended. Expiry, signing out in
  another tab, or a server restart all look like this. The event lets `main.js` show the sign-in
  screen without every view handling it.

### `WebUI/src/main.js` changes

- **`showApp(me)`.** Hides and empties `#login`, shows `#app`, mounts the profile, then starts the
  router on the first sign-in or re-renders the current screen on later ones.
- **`showSignIn(options)`.** Hides `#app` and **empties `#view`**, so the next person never sees
  the previous account's messages. Then it shows the card.
- **`signOut()`.** `POST /api/logout`, then the card with "You signed out." The `.catch` means a
  failed logout request still returns you to the sign-in screen.
- **`mail:signedout` listener.** Shows "Your session ended. Sign in again." It acts only if the
  card is not already showing, because several requests can fail with 401 at the same time, and
  redrawing the form each time would wipe what you are typing.
- **Startup.**
  1. `mountThemeToggle()` and `watchHealth()` work on both screens.
  2. `await api('/me')`. Signed in → the app. `unauthenticated` → the card. Anything else (API
     not running) → the card with that error. `await` at the top level works because `main.js`
     is a module.

### Checked in the browser (Brave + Playwright, against `serve -web dist`)

| Check | Result |
|---|---|
| Opening the app signed out | login card, app hidden |
| Wrong password | "Login failed: wrong username or password.", password field cleared |
| Sign in as alice | inbox loads, picture shows "A" |
| Picture menu | opens (`aria-expanded=true`), Escape closes it |
| 3 MB picture | "Pictures must be 2 MB or smaller." |
| 430-byte PNG | shown in the circle at once |
| Theme button | page turns light, `localStorage.theme = light`, `aria-pressed=false`, sun icon |
| Reload | still light from the first frame (boot script) |
| Sign out | "You signed out.", the inbox list is gone from the page |
| Sign in as bob | bob's own initial "B", not alice's picture |
| Cookie removed while open (expired session) | back to sign-in with "Your session ended. Sign in again." |
| New browser, OS set to light, nothing stored | opens light |
| Server log | `Signed in`, `Profile picture changed`, `Signed out` events, no password anywhere |

`Tests/server/server_test.go` covers the same on the API side; see the table below.

---

## Tests · `Tests/`

All tests are black-box: they import `email-client/pkg/...` and use only exported names.

| File | What it proves |
|---|---|
| `config/config_test.go` | Valid file loads with defaults; `EMAIL_PASSWORD` wins over the file; missing server, bad port, bad `from`, unknown key, and non-JSON all fail with a `config:` error. |
| `logger/logger_test.go` | Password passed as `password`, `PASS`, via `With`, and inside a group never appears; exactly 4 `[REDACTED]`; normal fields still print. **Block 1 "done when".** |
| `smtp/smtp_test.go` | A fake SMTP server on `127.0.0.1:0` (random free port) records every line. The test checks the exact command order, two `RCPT TO`s, the dot-stuffed `..starts with dot` line, the `.` terminator, and `QUIT`. Also: a 550 on RCPT is reported, connection refused is reported, and a silent server times out in about 200 ms instead of hanging. |
| `pop3/pop3_test.go` | A fake POP3 server: STAT count, LIST numbers and sizes, RETR un-stuffs `..leading dot` and converts CRLF, `-ERR` for a missing message is a `*pop3.Error`, wrong password is a `PASS` error, a socket cut mid-RETR says "transfer interrupted", and the Debug trace shows `PASS ****` but never the password. |
| `message/message_test.go` | Build → Parse round trip keeps From/To/Subject (with `é`)/Date/body; a newline in the subject cannot add a `Bcc:` header; an encoded-word subject from another client decodes; text with no headers is rejected; `ParseRecipients` accepts `bob@x, Carol <carol@y> ,` and rejects empty, `bob`, and a list with one bad entry. |
| `mailbox/mailbox_test.go` | Save → open a fresh `Mailbox` on the same file (a restart) → entry 2 is intact; the next new message gets ID 3 even though its POP3 number is 1; the same message added twice is stored once; a corrupt file is an error. **Block 4 "done when".** |
| `integration/greenmail_test.go` | Against the real GreenMail: sends 3 messages with a unique subject tag, LISTs and RETRs everything, finds all 3 with correct From/Date and an intact `.line starting with a dot`, saves and reopens the mailbox. Also: a wrong password is a `*pop3.Error`. **Block 2 + 3 "done when".** |
| `cli/cli_test.go` | Builds the real CLI binary and drives it through stdin. Run 1: compose → check inbox → exit. Run 2 (a restart): the message is in View Local Messages. The log has the spec's events and never `PASS alice`. A second test feeds `9`, `abc` and `not-an-address` and checks each prints an error while the program keeps running. **Block 5 "done when".** `TestErrorCases`: wrong password (via `EMAIL_PASSWORD`), unreachable server, and a server that accepts but never answers (1 s timeout). Each prints its headline, the program exits normally, and the wrong password is not in the log. **Block 9 "done when".** |
| `mailerr/mailerr_test.go` | Fake servers produce each failure for real: closed port, `.invalid` hostname, silent server, `-ERR` on PASS, `550` on RCPT, an HTTP reply to POP3, garbage to SMTP, a socket cut mid-RETR, a socket cut after the greeting, a bad address. Each must classify to the expected kind and have a headline. |
| `perf/perf_test.go` | Not a pass/fail test: the timing measurements for Block 10. Skipped unless `PERF=1`. |
| `server/server_test.go` | Each test is a "browser": an `httptest` server plus a client with its own cookie jar, signed in first. **Login flow**: signed out → 401 `unauthenticated`; health still works; wrong password → 401 `auth_failed`; bad address / empty password / non-JSON → 400; sign in → `/me` is alice; a second browser signs in as bob and sees bob while alice stays alice; logging alice out leaves bob signed in. **Avatar**: no picture → 404; PNG upload → GET returns the same bytes as `image/png`; bob does not see alice's picture; an SVG-with-script upload → 400; 3 MB → 400 "2 MB"; uploading signed out → 401. A real-POP3 login with the mail server down → 502 `unreachable`, not "wrong password". `httptest` runs the real handler. Round trip against GreenMail: health up → send → inbox has it → `/messages/{num}` body intact → a second refresh adds 0 → local matches. 12 parallel refresh/local/send requests all return 200 (run with `-race` to check for data races). Bad input gives 400 `invalid_input`; GET on `/api/send` gives 405. Servers down: refresh and send give 502 `unreachable` and health says `down`. A silent server gives 504 `timeout` in about 1 s. A wrong password gives 502 `auth_failed` and is not in the Debug log. **Block 6 "done when".** |

Run `go test -race ./...` to also check the locking.

The `integration`, `cli` and `server` round-trip tests call `t.Skip` when GreenMail is not reachable, so `go test
./...` still passes without Docker. Start the container first (`docker start greenmail`) to run
them for real.
