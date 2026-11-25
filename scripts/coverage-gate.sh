#!/usr/bin/env bash
# coverage-gate.sh <coverprofile>
#
# Fails when the total statement coverage is below TOTAL_FLOOR, or when any
# file in CRITICAL is less than fully covered. The critical files are the
# trust boundaries: who may connect, what reaches the terminal, what leaves
# the process for a notification destination. A branch there that no test
# runs is a refusal nothing proves, so the bar for them is every statement.
#
# Run it after `go test -coverprofile=<file> ./...`; `task cover` does both.
set -euo pipefail

profile=${1:?usage: coverage-gate.sh <coverprofile>}

# A ratchet, not a target: raise it when the total moves up, never lower it
# to go green.
TOTAL_FLOOR=85.0

MODULE=github.com/stxkxs/mkt
CRITICAL=(
	cmd/listen.go             # --listen bind and token refusals
	cmd/serve_auth.go         # SSH public-key allowlist
	internal/api/auth.go      # bearer-token check on the HTTP API
	internal/alert/redact.go  # destination redaction in logs and errors
	internal/alert/notify_args.go # argv built for the desktop notifier
	internal/alert/webhook.go # outbound webhook delivery
	internal/textsafe/textsafe.go # terminal-escape sanitizer
)

# Per file: covered and total statements. A block listed more than once
# (several test binaries covering one package) counts as covered if any
# listing ran it.
per_file=$(awk 'NR > 1 {
	split($1, loc, ":"); n = $2; c = $3
	if (!($1 in seen)) { seen[$1] = 1; total[loc[1]] += n }
	if (c > 0 && !($1 in hit)) { hit[$1] = 1; covered[loc[1]] += n }
} END {
	for (f in total) printf "%s %d %d\n", f, covered[f] + 0, total[f]
}' "$profile")

fail=0

total=$(awk '{ c += $2; t += $3 } END { if (t == 0) print 0; else printf "%.1f", 100 * c / t }' <<<"$per_file")
echo "total coverage: ${total}% (floor ${TOTAL_FLOOR}%)"
if awk -v t="$total" -v f="$TOTAL_FLOOR" 'BEGIN { exit !(t < f) }'; then
	echo "::error::total coverage ${total}% is below the ${TOTAL_FLOOR}% floor"
	fail=1
fi

for f in "${CRITICAL[@]}"; do
	line=$(awk -v f="$MODULE/$f" '$1 == f' <<<"$per_file")
	if [[ -z "$line" ]]; then
		# A file missing from the profile was never compiled into a test
		# binary, which is a gap, not a pass.
		echo "::error file=$f::not in the coverage profile"
		fail=1
		continue
	fi
	read -r _ covered stmts <<<"$line"
	if ((covered < stmts)); then
		echo "::error file=$f::$covered of $stmts statements covered; this file must be fully covered"
		fail=1
	else
		echo "fully covered: $f ($stmts statements)"
	fi
done

exit "$fail"
