#!/usr/bin/env bash
# Produces the spec §5 evidence in Results/.
#   Tests/section5.sh [--reset] [--no-go-tests]
#   --reset        restart GreenMail first, so the inbox starts empty
#   --no-go-tests  skip the Go test suite (it sends extra mail to alice)
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
OUT=$ROOT/Results
RUN=$OUT/run
RESET=0 GOTESTS=1
for arg in "$@"; do
  case $arg in
    --reset) RESET=1 ;;
    --no-go-tests) GOTESTS=0 ;;
    *) echo "unknown option $arg" >&2; exit 2 ;;
  esac
done

wait_ports() {
  for _ in $(seq 1 30); do
    nc -z localhost 3025 2>/dev/null && nc -z localhost 3110 2>/dev/null && return 0
    sleep 1
  done
  echo "GreenMail did not come up" >&2; exit 1
}

if [[ $RESET == 1 ]]; then
  echo "Restarting GreenMail (empties every inbox)…"
  docker restart greenmail >/dev/null
fi
wait_ports
sleep 1

rm -rf "$RUN"
mkdir -p "$RUN"
cp "$ROOT/Client/config.json" "$RUN/config.json"
(cd "$ROOT/Client" && go build -o "$RUN/emailcli" ./cmd/cli) || exit 1
PASSWORD=$(sed -n 's/.*"password": *"\([^"]*\)".*/\1/p' "$RUN/config.json")

# session TITLE INPUT [VAR=value ...] [-- CLI flags...]
session() {
  local title=$1 input=$2; shift 2
  local envs=() flags=()
  while [[ $# -gt 0 && $1 != -- ]]; do envs+=("$1"); shift; done
  [[ ${1:-} == -- ]] && shift && flags=("$@")
  echo "=== $title ==="
  echo "--- typed input ---"
  printf '%s' "$input"
  echo "--- program output ---"
  local start=$SECONDS
  (cd "$RUN" && printf '%s' "$input" | env "${envs[@]+"${envs[@]}"}" ./emailcli -debug "${flags[@]+"${flags[@]}"}" 2>&1)
  echo "--- exit code $?, about $((SECONDS - start)) s ---"
  echo
}

echo "1/7 connection handshakes"
{
  echo '$ printf "EHLO section5\r\nQUIT\r\n" | nc localhost 3025'
  printf 'EHLO section5\r\nQUIT\r\n' | nc -w 3 localhost 3025
  echo
  echo '$ printf "USER alice@localhost\r\nPASS ****\r\nSTAT\r\nQUIT\r\n" | nc localhost 3110'
  printf 'USER alice@localhost\r\nPASS %s\r\nSTAT\r\nQUIT\r\n' "$PASSWORD" | nc -w 3 localhost 3110
} > "$OUT/01-connection.txt"

echo "2/7 send three messages, check inbox, read"
session "Send 3 plain-text messages, check inbox, read message 2 and 3" \
"3
alice@localhost
Section 5 test 1
First plain-text message.
.
3
alice@localhost, bob@localhost
Section 5 test 2
Second message, sent to two recipients.
.line that starts with a dot
.
3
alice@localhost
Section 5 test 3
Third message.
It has two lines.
.
1
2
2
2
3
5
" > "$OUT/02-send-receive-read.txt"

echo "3/7 restart and reopen saved messages"
session "New process: view local messages and open ID 3 (no server needed)" "4
3
5
" > "$OUT/03-reopen-after-restart.txt"
cp "$RUN/data/mailbox.json" "$OUT/mailbox.json"

echo "4/7 error cases"
sed 's/"smtp_port": *[0-9]*/"smtp_port": 1/; s/"pop3_port": *[0-9]*/"pop3_port": 1/' "$RUN/config.json" > "$RUN/config-down.json"
sed 's/"smtp_port": *[0-9]*/"smtp_port": 39999/; s/"pop3_port": *[0-9]*/"pop3_port": 39999/; s/"timeout_seconds": *[0-9]*/"timeout_seconds": 2/' "$RUN/config.json" > "$RUN/config-timeout.json"
python3 -c '
import socket, time
s = socket.socket(); s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(("127.0.0.1", 39999)); s.listen()
conns = [s.accept()[0] for _ in range(2)]
time.sleep(30)' &
SILENT=$!
sleep 0.5
{
  session "Error 1: wrong password" "1
5
" EMAIL_PASSWORD=wrong-password
  session "Error 2: server unavailable (ports point at nothing)" "1
3
alice@localhost
Not delivered
body
.
5
" -- -config config-down.json
  session "Error 3: timeout (server accepts but never answers, timeout_seconds = 2)" "1
5
" -- -config config-timeout.json
  session "Error 4: invalid user input" "9
2
abc
3
not-an-address
4
99
5
"
} > "$OUT/04-error-cases.txt"
kill $SILENT 2>/dev/null; wait $SILENT 2>/dev/null

echo "5/7 log check"
cp "$RUN/logs/client.log" "$OUT/client.log"
{
  echo "Log file: Results/client.log ($(wc -l < "$OUT/client.log" | tr -d ' ') lines)"
  printf 'Lines containing the real password command "PASS %s": %s\n' '****' "$(grep -c "PASS $PASSWORD" "$OUT/client.log")"
  echo "Lines containing the wrong password used in error 1: $(grep -c 'wrong-password' "$OUT/client.log")"
  echo "Lines with the masked command \"PASS ****\": $(grep -c 'PASS \*\*\*\*' "$OUT/client.log")"
  echo
  echo "Spec example events found in the log:"
  for e in "Connected to SMTP server" "Message sent successfully" "Connected to POP3 server" \
           "Retrieved message" "POP3 authentication failed" "did not answer in time" "Cannot reach the mail server"; do
    printf '  %-30s %s\n' "$e" "$(grep -c "$e" "$OUT/client.log")"
  done
} > "$OUT/05-log-check.txt"

echo "6/7 timing (sends 30 messages to bob)"
(cd "$ROOT/Tests" && PERF=1 go test -count=1 -run TestTiming ./perf >/dev/null) || echo "timing run failed" >&2

if [[ $GOTESTS == 1 ]]; then
  echo "7/7 Go test suite"
  (cd "$ROOT/Tests" && go test -count=1 -v ./... 2>&1 | grep -E '^\s*--- |^ok|^FAIL|^PASS') > "$OUT/test-results.txt"
else
  echo "7/7 Go test suite skipped"
fi

rm -f "$RUN/emailcli"
echo "Done. Evidence is in $OUT"
