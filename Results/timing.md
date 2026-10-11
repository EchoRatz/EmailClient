# Timing

Measured 2026-10-09 16:14 · go1.26.7 darwin/arm64 · GreenMail in Docker on localhost · 10 runs per row (inbox check: 5).
Each time covers a whole protocol session including the TCP connect and QUIT.

| Operation | Message size | Min (ms) | Median (ms) | Mean (ms) | Max (ms) |
|---|---|---:|---:|---:|---:|
| Send (SMTP session) | 1 KB | 4.9 | 7.8 | 11.9 | 28.3 |
| Send (SMTP session) | 10 KB | 5.8 | 7.3 | 8.2 | 18.9 |
| Send (SMTP session) | 100 KB | 18.2 | 21.4 | 21.1 | 28.2 |
| Retrieve one (POP3 RETR) | 1 KB | 2.6 | 3.0 | 3.1 | 4.1 |
| Retrieve one (POP3 RETR) | 10 KB | 8.2 | 8.7 | 8.8 | 10.6 |
| Retrieve one (POP3 RETR) | 100 KB | 63.7 | 66.1 | 66.1 | 68.3 |
| Check inbox (LIST + RETR all 31) | mixed | 721.5 | 734.8 | 748.5 | 808.0 |
