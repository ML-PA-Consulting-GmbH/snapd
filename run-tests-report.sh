#!/usr/bin/env bash
# run-tests-report: run snapd unit/static tests and generate a structured report.
#
# Usage:
#   ./run-tests-report.sh [MODE] [OPTIONS]
#
# Modes (default: all):
#   all              Run static checks + unit tests
#   --static         Static checks only
#   --unit           Go + Python unit tests
#   --short-unit     Unit tests with -short flag (faster, skips integration-style subtests)
#
# Options:
#   --html           Also write an HTML report (test-report.html)
#   --markdown       Also write a Markdown report (test-report.md)
#   --out=DIR        Directory for report artifacts (default: .test-report)
#   --no-coverage    Skip coverage collection (faster)
#   --race           Enable Go race detector
#   --tags=TAGS      Extra Go build tags (comma-separated)
#   --timeout=MIN    Test timeout in minutes (default: 15)
#
# Environment variables mirror those accepted by run-checks:
#   TIMEOUT, GO_BUILD_TAGS, GO_TEST_RACE, SKIP_COVERAGE, COVERAGE_OUT

set -euo pipefail
export LANG=C.UTF-8
export LANGUAGE=en

# ─── defaults ────────────────────────────────────────────────────────────────
TIMEOUT=${TIMEOUT:-15}
COVERMODE=${COVERMODE:-atomic}
GO_BUILD_TAGS=${GO_BUILD_TAGS:-}
GO_TEST_RACE=${GO_TEST_RACE:-}
SKIP_COVERAGE=${SKIP_COVERAGE:-}
REPORT_DIR=${REPORT_DIR:-.test-report}
WRITE_HTML=
WRITE_MARKDOWN=

MODE=all
SHORT=

# ─── argument parsing ────────────────────────────────────────────────────────
for arg in "$@"; do
    case "$arg" in
    all)               MODE=all ;;
    --static)          MODE=static ;;
    --unit)            MODE=unit ;;
    --short-unit)      MODE=unit; SHORT="-short" ;;
    --html)            WRITE_HTML=1 ;;
    --markdown)        WRITE_MARKDOWN=1 ;;
    --out=*)           REPORT_DIR="${arg#--out=}" ;;
    --no-coverage)     SKIP_COVERAGE=1 ;;
    --race)            GO_TEST_RACE=1 ;;
    --tags=*)          GO_BUILD_TAGS="${arg#--tags=}" ;;
    --timeout=*)       TIMEOUT="${arg#--timeout=}" ;;
    -h|--help)
        sed -n '2,/^set -/{ /^#/{ s/^# \{0,1\}//; p }; /^set -/q }' "$0"
        exit 0
        ;;
    *)
        echo "Unknown argument: $arg  (use --help)" >&2
        exit 1
        ;;
    esac
done

COVERAGE_OUT=${COVERAGE_OUT:-$REPORT_DIR/coverage.cov}

# ─── temp files cleanup ──────────────────────────────────────────────────────
_TMPFILES=()
_tmpfile() {
    local f; f=$(mktemp --suffix="${1:-.tmp}")
    _TMPFILES+=("$f")
    echo "$f"
}
trap 'rm -f "${_TMPFILES[@]:-}"' EXIT

# ─── colours ─────────────────────────────────────────────────────────────────
if [ -t 1 ]; then
    RED=$'\033[0;31m'; GREEN=$'\033[0;32m'; YELLOW=$'\033[0;33m'
    BLUE=$'\033[0;34m'; BOLD=$'\033[1m'; RESET=$'\033[0m'
else
    RED=; GREEN=; YELLOW=; BLUE=; BOLD=; RESET=
fi

log()  { echo "${BLUE}▸${RESET} $*"; }
ok()   { echo "${GREEN}✔${RESET} $*"; }
fail() { echo "${RED}✘${RESET} $*"; }
warn() { echo "${YELLOW}!${RESET} $*"; }
hr()   { printf '%0.s─' {1..72}; echo; }

# ─── timing helper ───────────────────────────────────────────────────────────
elapsed_since() {
    local start=$1
    local end; end=$(date +%s)
    echo $(( end - start ))
}

fmt_duration() {
    local secs=$1
    if (( secs >= 60 )); then
        printf '%dm%02ds' $(( secs / 60 )) $(( secs % 60 ))
    else
        printf '%ds' "$secs"
    fi
}

# ─── result tracking ─────────────────────────────────────────────────────────
STATIC_STATUS=skipped
GOTEST_STATUS=skipped
PYTEST_STATUS=skipped
PYTOOLS_STATUS=skipped

STATIC_DURATION=0
GOTEST_DURATION=0
PYTEST_DURATION=0
PYTOOLS_DURATION=0

GO_PACKAGES_PASS=0
GO_PACKAGES_FAIL=0
GO_TESTS_PASS=0
GO_TESTS_FAIL=0
GO_TESTS_SKIP=0

mkdir -p "$REPORT_DIR"
GOTEST_JSON="$REPORT_DIR/go-test.ndjson"
STATIC_LOG="$REPORT_DIR/static.log"
PYTEST_LOG="$REPORT_DIR/pytest.log"
PYTOOLS_LOG="$REPORT_DIR/pytools.log"

# ─── section: static checks ──────────────────────────────────────────────────
run_static() {
    log "Running static checks (delegating to run-checks --static)"
    hr
    local t0; t0=$(date +%s)

    # Pipe through tee so the user sees live output AND we capture it
    if SKIP_COVERAGE=1 TIMEOUT="$TIMEOUT" \
       GO_BUILD_TAGS="$GO_BUILD_TAGS" \
       ./run-checks --static 2>&1 | tee "$STATIC_LOG"; then
        STATIC_STATUS=pass
        ok "Static checks passed ($(fmt_duration "$(elapsed_since "$t0")"))"
    else
        STATIC_STATUS=fail
        fail "Static checks FAILED ($(fmt_duration "$(elapsed_since "$t0")"))"
    fi
    STATIC_DURATION=$(elapsed_since "$t0")
    hr
}

# ─── section: Go unit tests ──────────────────────────────────────────────────
run_go_tests() {
    log "Running Go unit tests"
    hr

    local t0; t0=$(date +%s)
    local tags= race= timeout="${TIMEOUT}m" coverage=

    [ -n "$GO_BUILD_TAGS" ] && tags="-tags $GO_BUILD_TAGS"
    if [ -n "$GO_TEST_RACE" ]; then
        race="-race"
        timeout="$((TIMEOUT * 2))m"
    fi

    if [ -z "$SKIP_COVERAGE" ]; then
        mkdir -p "$(dirname "$COVERAGE_OUT")"
        coverage="-coverprofile=$COVERAGE_OUT -covermode=$COVERMODE"
    fi

    log "go build ./..."
    # shellcheck disable=SC2086
    go build -v $tags $race github.com/snapcore/snapd/... 2>&1 | \
        grep -v '^#' | head -20 || true  # suppress noisy build lines

    log "go test -json ./...  (timeout=${timeout})"

    # Write live pretty-printer to a temp file (heredoc can't share stdin with a pipeline)
    local pretty_py; pretty_py=$(_tmpfile .py)
    cat > "$pretty_py" <<'PYEOF'
import sys, json

for line in sys.stdin:
    line = line.rstrip('\n')
    if not line:
        continue
    try:
        ev = json.loads(line)
    except json.JSONDecodeError:
        print(line)
        continue

    action  = ev.get('Action', '')
    pkg     = ev.get('Package', '')
    test    = ev.get('Test', '')
    output  = ev.get('Output', '').rstrip('\n')
    elapsed = ev.get('Elapsed', 0)

    if action == 'fail' and not test:
        print(f"\033[0;31m✘\033[0m FAIL  {pkg}  ({elapsed:.2f}s)", flush=True)
    elif action == 'pass' and not test:
        print(f"\033[0;32m✔\033[0m ok    {pkg}  ({elapsed:.2f}s)", flush=True)
    elif action == 'fail' and test:
        print(f"\033[0;31m  ✘ FAIL: {test}\033[0m", flush=True)
    elif action == 'output' and test and ('--- FAIL' in output or 'FAIL\t' in output):
        print(f"    {output}", flush=True)
    # suppress skip/run noise; all details are captured in the NDJSON file
PYEOF

    # Run with -json for machine-readable output; tee raw NDJSON for report
    local go_test_rc=0
    # shellcheck disable=SC2086
    GOTRACEBACK=1 go test $SHORT -json $tags $race -timeout "$timeout" $coverage ./... \
        2>&1 | tee "$GOTEST_JSON" | python3 -u "$pretty_py" || go_test_rc=$?

    GOTEST_DURATION=$(elapsed_since "$t0")

    if [ "$go_test_rc" -eq 0 ]; then
        GOTEST_STATUS=pass
        ok "Go unit tests passed ($(fmt_duration "$GOTEST_DURATION"))"
    else
        GOTEST_STATUS=fail
        fail "Go unit tests FAILED ($(fmt_duration "$GOTEST_DURATION"))"
    fi
    hr
}

# ─── section: Python tests ───────────────────────────────────────────────────
run_python_tests() {
    hr
    # mountinfo.query and version-compare built-in unit tests
    if command -v python3 >/dev/null 2>&1; then
        log "Running Python tool unit tests"
        local t0; t0=$(date +%s)
        local pytools_rc=0
        {
            python3 ./tests/lib/tools/mountinfo.query --run-unit-tests
            python3 ./tests/lib/tools/version-compare --run-unit-tests
        } 2>&1 | tee "$PYTOOLS_LOG" || pytools_rc=$?
        PYTOOLS_DURATION=$(elapsed_since "$t0")
        if [ "$pytools_rc" -eq 0 ]; then
            PYTOOLS_STATUS=pass
            ok "Python tool tests passed ($(fmt_duration "$PYTOOLS_DURATION"))"
        else
            PYTOOLS_STATUS=fail
            fail "Python tool tests FAILED ($(fmt_duration "$PYTOOLS_DURATION"))"
        fi
    else
        warn "python3 not found, skipping Python tool tests"
    fi

    # pytest for release-tools
    if command -v pytest-3 >/dev/null 2>&1; then
        log "Running pytest-3 (release-tools)"
        local t0; t0=$(date +%s)
        local pytest_rc=0
        PYTHONDONTWRITEBYTECODE=1 pytest-3 ./release-tools 2>&1 | tee "$PYTEST_LOG" || pytest_rc=$?
        PYTEST_DURATION=$(elapsed_since "$t0")
        if [ "$pytest_rc" -eq 0 ]; then
            PYTEST_STATUS=pass
            ok "pytest-3 passed ($(fmt_duration "$PYTEST_DURATION"))"
        else
            PYTEST_STATUS=fail
            fail "pytest-3 FAILED ($(fmt_duration "$PYTEST_DURATION"))"
        fi
    else
        warn "pytest-3 not found, skipping release-tools tests"
    fi
    hr
}

# ─── section: parse Go JSON and extract stats ────────────────────────────────
parse_go_json() {
    [ -f "$GOTEST_JSON" ] || return
    python3 - "$GOTEST_JSON" <<'PYEOF'
import sys, json, collections

ndjson_path = sys.argv[1]

pkg_results = {}   # pkg -> {pass, fail, skip, elapsed, failures: [name]}
test_results = {}  # (pkg, test) -> action

with open(ndjson_path) as f:
    for line in f:
        line = line.strip()
        if not line:
            continue
        try:
            ev = json.loads(line)
        except json.JSONDecodeError:
            continue
        action  = ev.get('Action', '')
        pkg     = ev.get('Package', '')
        test    = ev.get('Test', '')
        elapsed = ev.get('Elapsed', 0)
        output  = ev.get('Output', '')

        if pkg not in pkg_results:
            pkg_results[pkg] = {'pass': 0, 'fail': 0, 'skip': 0, 'elapsed': 0.0, 'failures': []}

        if not test:
            # package-level event
            if action in ('pass', 'fail'):
                pkg_results[pkg]['elapsed'] = elapsed
        else:
            # individual test event
            if action == 'pass':
                pkg_results[pkg]['pass'] += 1
            elif action == 'fail':
                pkg_results[pkg]['fail'] += 1
                pkg_results[pkg]['failures'].append(test)
            elif action == 'skip':
                pkg_results[pkg]['skip'] += 1

total_pass = sum(v['pass'] for v in pkg_results.values())
total_fail = sum(v['fail'] for v in pkg_results.values())
total_skip = sum(v['skip'] for v in pkg_results.values())
pkg_pass   = sum(1 for v in pkg_results.values() if v['fail'] == 0)
pkg_fail   = sum(1 for v in pkg_results.values() if v['fail'] > 0)

# Write stats for the parent shell to source
with open('/tmp/_snapd_test_stats.sh', 'w') as f:
    f.write(f'GO_TESTS_PASS={total_pass}\n')
    f.write(f'GO_TESTS_FAIL={total_fail}\n')
    f.write(f'GO_TESTS_SKIP={total_skip}\n')
    f.write(f'GO_PACKAGES_PASS={pkg_pass}\n')
    f.write(f'GO_PACKAGES_FAIL={pkg_fail}\n')

# Print failed packages and their failing tests
if pkg_fail > 0:
    print('\nFailed packages:')
    for pkg, r in sorted(pkg_results.items()):
        if r['fail'] > 0:
            print(f'  {pkg}')
            for t in r['failures']:
                print(f'    ✘ {t}')

PYEOF
    # shellcheck disable=SC1091
    [ -f /tmp/_snapd_test_stats.sh ] && source /tmp/_snapd_test_stats.sh && rm -f /tmp/_snapd_test_stats.sh
}

# ─── report generators ───────────────────────────────────────────────────────
status_icon() {
    case "$1" in
    pass)    echo "✔ PASS" ;;
    fail)    echo "✘ FAIL" ;;
    skipped) echo "– SKIP" ;;
    *)       echo "? ????" ;;
    esac
}

md_status() {
    case "$1" in
    pass)    echo "✅" ;;
    fail)    echo "❌" ;;
    skipped) echo "⏭️" ;;
    *)       echo "❓" ;;
    esac
}

print_terminal_report() {
    local total_duration=$(( STATIC_DURATION + GOTEST_DURATION + PYTOOLS_DURATION + PYTEST_DURATION ))
    echo
    echo "${BOLD}╔══════════════════════════════════════════════════════════════════════╗${RESET}"
    echo "${BOLD}║                       snapd test report                             ║${RESET}"
    echo "${BOLD}╚══════════════════════════════════════════════════════════════════════╝${RESET}"
    echo
    printf "${BOLD}%-30s %-10s %s${RESET}\n" "Suite" "Result" "Duration"
    hr
    printf "%-30s %-10s %s\n" "Static checks"      "$(status_icon "$STATIC_STATUS")"  "$(fmt_duration "$STATIC_DURATION")"
    printf "%-30s %-10s %s\n" "Go unit tests"       "$(status_icon "$GOTEST_STATUS")"  "$(fmt_duration "$GOTEST_DURATION")"
    printf "%-30s %-10s %s\n" "Python tool tests"   "$(status_icon "$PYTOOLS_STATUS")" "$(fmt_duration "$PYTOOLS_DURATION")"
    printf "%-30s %-10s %s\n" "pytest-3 (release)"  "$(status_icon "$PYTEST_STATUS")"  "$(fmt_duration "$PYTEST_DURATION")"
    hr
    printf "${BOLD}%-30s %-10s %s${RESET}\n" "Total" "" "$(fmt_duration "$total_duration")"
    echo

    if [ "$GOTEST_STATUS" != skipped ]; then
        echo "${BOLD}Go test summary:${RESET}"
        printf "  Packages  pass=%-5s fail=%s\n" "$GO_PACKAGES_PASS" "$GO_PACKAGES_FAIL"
        printf "  Tests     pass=%-5s fail=%-5s skip=%s\n" "$GO_TESTS_PASS" "$GO_TESTS_FAIL" "$GO_TESTS_SKIP"
        if [ -z "$SKIP_COVERAGE" ] && [ -f "$COVERAGE_OUT" ]; then
            local cov
            cov=$(go tool cover -func="$COVERAGE_OUT" 2>/dev/null | grep '^total:' | awk '{print $3}') || true
            [ -n "$cov" ] && echo "  Coverage  $cov"
        fi
        echo
    fi

    # Overall result
    local overall=pass
    for s in "$STATIC_STATUS" "$GOTEST_STATUS" "$PYTOOLS_STATUS" "$PYTEST_STATUS"; do
        [ "$s" = fail ] && overall=fail
    done

    if [ "$overall" = pass ]; then
        echo "${GREEN}${BOLD}All tests passed.${RESET}"
    else
        echo "${RED}${BOLD}Some tests FAILED. Check $REPORT_DIR/ for details.${RESET}"
        parse_go_json
    fi
    echo
}

write_markdown_report() {
    local out="$REPORT_DIR/test-report.md"
    local total_duration=$(( STATIC_DURATION + GOTEST_DURATION + PYTOOLS_DURATION + PYTEST_DURATION ))
    local ts; ts=$(date '+%Y-%m-%d %H:%M:%S %Z')
    local branch; branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)
    local commit; commit=$(git rev-parse --short HEAD 2>/dev/null || echo unknown)

    {
    cat <<MD
# snapd Test Report

**Branch:** \`$branch\`  **Commit:** \`$commit\`  **Date:** $ts

## Suite Results

| Suite | Result | Duration |
|---|---|---|
| Static checks | $(md_status "$STATIC_STATUS") | $(fmt_duration "$STATIC_DURATION") |
| Go unit tests | $(md_status "$GOTEST_STATUS") | $(fmt_duration "$GOTEST_DURATION") |
| Python tool tests | $(md_status "$PYTOOLS_STATUS") | $(fmt_duration "$PYTOOLS_DURATION") |
| pytest-3 (release-tools) | $(md_status "$PYTEST_STATUS") | $(fmt_duration "$PYTEST_DURATION") |
| **Total** | | **$(fmt_duration "$total_duration")** |

MD

    if [ "$GOTEST_STATUS" != skipped ]; then
        cat <<MD
## Go Test Statistics

| Metric | Value |
|---|---|
| Packages passed | $GO_PACKAGES_PASS |
| Packages failed | $GO_PACKAGES_FAIL |
| Tests passed | $GO_TESTS_PASS |
| Tests failed | $GO_TESTS_FAIL |
| Tests skipped | $GO_TESTS_SKIP |
MD
        if [ -z "$SKIP_COVERAGE" ] && [ -f "$COVERAGE_OUT" ]; then
            local cov
            cov=$(go tool cover -func="$COVERAGE_OUT" 2>/dev/null | grep '^total:' | awk '{print $3}') || true
            [ -n "$cov" ] && echo "| Total coverage | $cov |"
        fi
        echo
    fi

    # Failed test details from JSON
    if [ -f "$GOTEST_JSON" ] && [ "$GOTEST_STATUS" = fail ]; then
        echo "## Failed Tests"
        echo
        python3 - "$GOTEST_JSON" <<'PYEOF'
import sys, json

with open(sys.argv[1]) as f:
    lines = f.readlines()

pkg_failures = {}
for line in lines:
    try:
        ev = json.loads(line)
    except Exception:
        continue
    if ev.get('Action') == 'fail' and ev.get('Test'):
        pkg = ev.get('Package', '')
        pkg_failures.setdefault(pkg, []).append(ev['Test'])

for pkg in sorted(pkg_failures):
    print(f"### `{pkg}`\n")
    for t in pkg_failures[pkg]:
        print(f"- `{t}`")
    print()
PYEOF
    fi

    # Collect failure output snippets
    if [ -f "$GOTEST_JSON" ] && [ "$GOTEST_STATUS" = fail ]; then
        echo "## Failure Output"
        echo
        echo '```'
        python3 - "$GOTEST_JSON" <<'PYEOF'
import sys, json

# Print output lines that belong to failing tests
with open(sys.argv[1]) as f:
    lines = f.readlines()

failed_tests = set()
for line in lines:
    try:
        ev = json.loads(line)
    except Exception:
        continue
    if ev.get('Action') == 'fail' and ev.get('Test'):
        failed_tests.add((ev.get('Package',''), ev['Test']))

for line in lines:
    try:
        ev = json.loads(line)
    except Exception:
        continue
    if ev.get('Action') == 'output':
        key = (ev.get('Package',''), ev.get('Test',''))
        if key in failed_tests:
            out = ev.get('Output', '').rstrip('\n')
            if out:
                print(out)
PYEOF
        echo '```'
        echo
    fi

    } > "$out"

    ok "Markdown report written to $out"
}

write_html_report() {
    local out="$REPORT_DIR/test-report.html"
    local total_duration=$(( STATIC_DURATION + GOTEST_DURATION + PYTOOLS_DURATION + PYTEST_DURATION ))
    local ts; ts=$(date '+%Y-%m-%d %H:%M:%S %Z')
    local branch; branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)
    local commit; commit=$(git rev-parse --short HEAD 2>/dev/null || echo unknown)

    python3 - "$GOTEST_JSON" \
        "$STATIC_STATUS" "$GOTEST_STATUS" "$PYTOOLS_STATUS" "$PYTEST_STATUS" \
        "$STATIC_DURATION" "$GOTEST_DURATION" "$PYTOOLS_DURATION" "$PYTEST_DURATION" \
        "$GO_PACKAGES_PASS" "$GO_PACKAGES_FAIL" \
        "$GO_TESTS_PASS" "$GO_TESTS_FAIL" "$GO_TESTS_SKIP" \
        "$total_duration" "$ts" "$branch" "$commit" "$out" <<'PYEOF'
import sys, json, html as html_mod

(ndjson, s_static, s_go, s_py, s_pytest,
 d_static, d_go, d_py, d_pytest,
 pkg_pass, pkg_fail,
 t_pass, t_fail, t_skip,
 total_dur, ts, branch, commit, out_path) = sys.argv[1:]

def fmt_dur(s):
    s = int(s)
    return f"{s//60}m{s%60:02d}s" if s >= 60 else f"{s}s"

def badge(status):
    colors = {'pass': '#22c55e', 'fail': '#ef4444', 'skipped': '#94a3b8'}
    labels = {'pass': 'PASS', 'fail': 'FAIL', 'skipped': 'SKIP'}
    c = colors.get(status, '#94a3b8')
    l = labels.get(status, '???')
    return f'<span style="background:{c};color:#fff;padding:2px 8px;border-radius:4px;font-size:12px;font-weight:bold">{l}</span>'

# Parse failures from NDJSON
pkg_failures = {}
failed_output = {}
try:
    with open(ndjson) as f:
        for line in f:
            try:
                ev = json.loads(line)
            except Exception:
                continue
            if ev.get('Action') == 'fail' and ev.get('Test'):
                pkg = ev.get('Package', '')
                pkg_failures.setdefault(pkg, []).append(ev['Test'])
            if ev.get('Action') == 'output' and ev.get('Test'):
                key = (ev.get('Package',''), ev.get('Test',''))
                failed_output.setdefault(key, []).append(ev.get('Output',''))
except FileNotFoundError:
    pass

rows = [
    ('Static checks',            s_static,  d_static),
    ('Go unit tests',            s_go,      d_go),
    ('Python tool tests',        s_py,      d_py),
    ('pytest-3 (release-tools)', s_pytest,  d_pytest),
]

suite_rows = ''.join(
    f'<tr><td>{name}</td><td>{badge(st)}</td><td>{fmt_dur(dur)}</td></tr>'
    for name, st, dur in rows
)

go_stats = f'''
<table>
<tr><th>Metric</th><th>Value</th></tr>
<tr><td>Packages passed</td><td>{pkg_pass}</td></tr>
<tr><td>Packages failed</td><td>{pkg_fail}</td></tr>
<tr><td>Tests passed</td><td class="pass">{t_pass}</td></tr>
<tr><td>Tests failed</td><td class="fail">{t_fail}</td></tr>
<tr><td>Tests skipped</td><td>{t_skip}</td></tr>
</table>
''' if s_go != 'skipped' else ''

fail_section = ''
if pkg_failures:
    items = ''
    for pkg in sorted(pkg_failures):
        tests = ''.join(f'<li><code>{html_mod.escape(t)}</code></li>' for t in pkg_failures[pkg])
        items += f'<details open><summary><code>{html_mod.escape(pkg)}</code></summary><ul>{tests}</ul></details>'
    fail_section = f'<h2>Failed Tests</h2>{items}'

overall = 'PASS' if all(s in ('pass','skipped') for s in [s_static,s_go,s_py,s_pytest]) else 'FAIL'
overall_color = '#22c55e' if overall == 'PASS' else '#ef4444'

html = f'''<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>snapd Test Report</title>
<style>
  body {{ font-family: system-ui,sans-serif; max-width: 900px; margin: 2rem auto; padding: 0 1rem; color: #1e293b; }}
  h1 {{ border-bottom: 2px solid #e2e8f0; padding-bottom: .5rem; }}
  h2 {{ margin-top: 2rem; color: #334155; }}
  table {{ border-collapse: collapse; width: 100%; margin: 1rem 0; }}
  th, td {{ text-align: left; padding: .5rem .75rem; border: 1px solid #e2e8f0; }}
  th {{ background: #f8fafc; font-weight: 600; }}
  .pass {{ color: #16a34a; font-weight: bold; }}
  .fail {{ color: #dc2626; font-weight: bold; }}
  .overall {{ font-size: 1.5rem; font-weight: bold; color: {overall_color}; margin: 1rem 0; }}
  code {{ background: #f1f5f9; padding: 1px 4px; border-radius: 3px; font-size: .9em; }}
  details {{ margin: .5rem 0; }}
  summary {{ cursor: pointer; font-weight: 500; }}
  ul {{ margin: .25rem 0 .25rem 1.5rem; }}
  .meta {{ color: #64748b; font-size: .9rem; margin-bottom: 1.5rem; }}
</style>
</head>
<body>
<h1>snapd Test Report</h1>
<div class="meta">
  <strong>Branch:</strong> <code>{html_mod.escape(branch)}</code> &nbsp;
  <strong>Commit:</strong> <code>{html_mod.escape(commit)}</code> &nbsp;
  <strong>Date:</strong> {html_mod.escape(ts)}
</div>
<div class="overall">Overall: {overall}</div>

<h2>Suite Results</h2>
<table>
<tr><th>Suite</th><th>Result</th><th>Duration</th></tr>
{suite_rows}
<tr><th>Total</th><th></th><th>{fmt_dur(total_dur)}</th></tr>
</table>

<h2>Go Test Statistics</h2>
{go_stats}

{fail_section}
</body>
</html>'''

with open(out_path, 'w') as f:
    f.write(html)

print(f"HTML report written to {out_path}")
PYEOF

    ok "HTML report written to $out"
}

# ─── main ─────────────────────────────────────────────────────────────────────
main() {
    echo
    echo "${BOLD}snapd test runner${RESET}  (mode=${MODE}${SHORT:+, short})"
    echo "$(date '+%Y-%m-%d %H:%M:%S %Z')  branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo ?)"
    hr
    echo "Report artifacts → $REPORT_DIR/"
    echo

    case "$MODE" in
    all)
        run_static
        run_go_tests
        run_python_tests
        ;;
    static)
        run_static
        ;;
    unit)
        run_go_tests
        run_python_tests
        ;;
    esac

    parse_go_json
    print_terminal_report

    [ -n "$WRITE_MARKDOWN" ] && write_markdown_report
    [ -n "$WRITE_HTML"     ] && write_html_report

    # Exit with failure if any suite failed
    for s in "$STATIC_STATUS" "$GOTEST_STATUS" "$PYTOOLS_STATUS" "$PYTEST_STATUS"; do
        [ "$s" = fail ] && exit 1
    done
    exit 0
}

main
