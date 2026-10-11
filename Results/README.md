# Test Results — Spec §5

Run on 2026-10-09 against GreenMail 2.1.14 (Docker, localhost) with `Tests/section5.sh --reset`.
GreenMail was restarted first, so the inbox started empty. Every file here can be regenerated
with the same command.

## Checklist

| # | §5 requirement | Result | Evidence |
|---|---|---|---|
| 1 | Connect to the SMTP and POP3 servers | **Pass.** SMTP: `220` greeting, `250` to EHLO, `221` to QUIT. POP3: `+OK` greeting, USER/PASS accepted, `STAT` → `+OK 0 0`. | `01-connection.txt`, `client.log` ("Connected to SMTP server", "Connected to POP3 server") |
| 2 | Send at least three plain-text messages | **Pass.** 3 messages from the CLI (53, 11, 6 ms); message 2 to two recipients. A 4th sent from the web UI. | `02-send-receive-read.txt`, `screenshots/web-03-compose.png`, `web-04-sent.png` |
| 3 | Retrieve and display received messages | **Pass.** Check Inbox lists all 3 with number, sender, subject, date. Read Message shows messages 2 and 3 in full; the body line `.line that starts with a dot` arrives intact (dot-stuffing works both ways). | `02-send-receive-read.txt`, `screenshots/web-01-inbox.png`, `web-02-read-message.png` |
| 4 | Save downloaded messages and reopen them | **Pass.** A new CLI process lists the 3 saved messages and opens ID 3 without contacting the server. | `03-reopen-after-restart.txt`, `mailbox.json`, `screenshots/web-06-saved.png`, `web-07-saved-message.png` |
| 5 | Handle at least three error cases | **Pass, 4 cases.** None crashes; every run exits with code 0. See the table below. | `04-error-cases.txt`, `screenshots/web-08-error-wrong-password.png`, `web-09-error-server-down.png` |
| – | Passwords never logged | **Pass.** 0 lines with the real or the wrong password; 4 lines with `PASS ****`. | `05-log-check.txt`, `client.log` |
| – | Automated tests | **Pass.** 51 tests pass with `go test -race ./...`, 0 fail. | `test-results.txt` |

### Error cases

| Case | How it was caused | What the user sees |
|---|---|---|
| Wrong password | `EMAIL_PASSWORD=wrong-password` | `Login failed: wrong username or password.` (detail: `-ERR ... Invalid password`) |
| Server unavailable | ports set to 1, nothing listening | `Cannot reach the mail server. Is it running, and are the address and port right?`, for both Check Inbox and Send |
| Timeout | a socket that accepts but never sends a greeting, `timeout_seconds: 2` | `The mail server did not answer in time.` after about 2 s |
| Invalid input | menu `9`, message number `abc`, recipient `not-an-address`, saved ID `99` | one specific message per input, then the menu again |

Two more failure kinds are covered by automated tests with fake servers (`Tests/mailerr`):
a connection cut in the middle of RETR ("The connection was lost in the middle of a transfer")
and a server that does not speak the protocol ("...a reply this client does not understand").

## Timing

From `timing.md` (10 runs per row; inbox check 5 runs). Each time is one whole protocol
session: TCP connect, commands, QUIT.

| Operation | Message size | Min (ms) | Median (ms) | Mean (ms) | Max (ms) |
|---|---|---:|---:|---:|---:|
| Send (SMTP session) | 1 KB | 4.9 | 7.8 | 11.9 | 28.3 |
| Send (SMTP session) | 10 KB | 5.8 | 7.3 | 8.2 | 18.9 |
| Send (SMTP session) | 100 KB | 18.2 | 21.4 | 21.1 | 28.2 |
| Retrieve one (POP3 RETR) | 1 KB | 2.6 | 3.0 | 3.1 | 4.1 |
| Retrieve one (POP3 RETR) | 10 KB | 8.2 | 8.7 | 8.8 | 10.6 |
| Retrieve one (POP3 RETR) | 100 KB | 63.7 | 66.1 | 66.1 | 68.3 |
| Check inbox (LIST + RETR all 31) | mixed | 721.5 | 734.8 | 748.5 | 808.0 |

Environment: Apple Silicon Mac, macOS, go1.26.7, GreenMail 2.1.14 in Docker Desktop, all on
localhost.

**Observations**

- **Small messages are dominated by fixed cost.** 1 KB and 10 KB sends both take about 7–8 ms.
  That is the connect plus five command round trips, not the data.
- **The first send is slower** (53 ms in the CLI run, 28 ms max in the table). The first
  connection warms up GreenMail's Java code and Docker's port forwarding. Later sessions take
  5–11 ms.
- **Retrieving 100 KB takes about 3× as long as sending 100 KB.** The cause is on the server
  side. A raw Python socket (no Go code involved) received a 104 KB RETR reply in **672 separate
  `recv()` calls averaging 155 bytes**: GreenMail writes roughly one line per TCP segment, and
  every segment crosses Docker's port forwarding. Sending is faster because the client buffers
  its writes into large segments. The client reassembled all those fragments correctly, which is
  the spec's point that one socket read is not one protocol response.
- **Check inbox grows with mailbox size.** 31 messages (10 of them 100 KB) take about 0.73 s
  because the client RETRs every message to show sender, subject and date. POP3 `TOP n 0`
  (headers only) would make this much cheaper. It is not in the required command set.

## Files

| File | Content |
|---|---|
| `01-connection.txt` | Raw SMTP and POP3 handshakes with `nc` (password shown as `****`) |
| `02-send-receive-read.txt` | CLI session: send 3, check inbox, read 2 messages (typed input, then output) |
| `03-reopen-after-restart.txt` | Second CLI process: saved messages still there |
| `04-error-cases.txt` | The four error cases above, each with input, output and exit code |
| `05-log-check.txt` | Password and event counts in the log |
| `client.log` | Representative log with Debug protocol trace (every `C:` / `S:` line) |
| `mailbox.json` | The local mailbox after the run |
| `timing.md` | Timing table as written by `Tests/perf` |
| `test-results.txt` | Every Go test and its result |
| `screenshots/` | Web UI: inbox, read, compose, sent, saved, both errors, phone width |
| `run/` | Working folder of the CLI runs (config copies, data, logs) |
