#!/usr/bin/env bash
# E2E Test: Robot Probe Command
# Covers: bd-3exkg - E2E Tests: Robot Probe

set -uo pipefail
# Note: Not using -e so that assertion failures don't cause early exit.
# Failures are tracked via log library and reported in summary.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/lib/log.sh"
set +e

TEST_PREFIX="e2e-probe-$$"
CREATED_SESSIONS=()
BEAD_ID="bd-3exkg"
E2E_TAG="[E2E-PROBE]"

cleanup() {
    log_section "Cleanup"
    for session in "${CREATED_SESSIONS[@]}"; do
        tmux kill-session -t "$session" 2>/dev/null || true
    done
    cleanup_sessions "${TEST_PREFIX}"
}
trap cleanup EXIT

spawn_test_session() {
    local session="$1"
    shift
    CREATED_SESSIONS+=("$session")
    ntm_spawn "$session" "$@"
}

find_shell_pane() {
    local session="$1"
    tmux list-panes -t "${session}:1" -F '#{pane_index} #{pane_current_command}' 2>/dev/null | \
        awk 'tolower($2) ~ /(bash|zsh|fish|sh)/ {print $1; exit}'
}

probe_output() {
    local session="$1"
    local panes="$2"
    shift 2
    log_exec ntm --robot-probe="$session" --panes="$panes" "$@"
    get_last_output
}

main() {
    log_init "test-robot-probe"

    require_ntm
    require_tmux
    require_jq

    local session="${TEST_PREFIX}-probe"
    log_info "${E2E_TAG} bead=${BEAD_ID} session=${session} step=spawn"
    if ! spawn_test_session "$session" --cc 1 --no-prompt; then
        log_error "Failed to spawn session for probe tests"
        log_summary
        return 1
    fi

    local shell_pane
    shell_pane=$(find_shell_pane "$session")
    log_assert_not_empty "$shell_pane" "shell pane detected for probe tests"
    if [[ -z "$shell_pane" ]]; then
        log_summary
        return 1
    fi

    log_section "Scenario 1: Responsive shell (keystroke_echo)"
    local output
    output=$(probe_output "$session" "$shell_pane")
    log_assert_valid_json "$output" "robot-probe returns valid JSON"
    log_assert_eq "$(get_last_exit_code)" "0" "responsive probe exit code is 0"
    log_assert_json_eq "$output" ".summary.total_probed" "1" "summary.total_probed == 1"
    log_assert_json_eq "$output" ".summary.responsive" "1" "summary.responsive == 1"
    log_assert_json_eq "$output" ".probes[0].responsive" "true" "probe is responsive"

    log_section "Scenario 2: Unresponsive pane (keystroke_echo)"
    tmux send-keys -t "${session}:1.${shell_pane}" "tail -f /dev/null" Enter
    sleep 0.2
    output=$(probe_output "$session" "$shell_pane" --probe-timeout=200)
    log_assert_valid_json "$output" "robot-probe returns valid JSON for unresponsive pane"
    log_assert_eq "$(get_last_exit_code)" "2" "unresponsive probe exit code is 2"
    log_assert_json_eq "$output" ".probes[0].responsive" "false" "probe marks pane unresponsive"
    log_assert_json_eq "$output" ".probes[0].recommendation" "likely_stuck" "recommendation likely_stuck"

    log_section "Scenario 3: Method comparison (interrupt_test vs keystroke_echo)"
    output=$(probe_output "$session" "$shell_pane" --probe-method=interrupt_test --probe-timeout=500)
    log_assert_valid_json "$output" "interrupt_test returns valid JSON"
    log_assert_eq "$(get_last_exit_code)" "0" "interrupt_test exit code is 0"
    log_assert_json_eq "$output" ".probes[0].responsive" "true" "interrupt_test shows responsive"
    log_assert_json_eq "$output" ".probes[0].probe_method" "interrupt_test" "probe_method is interrupt_test"

    log_section "Scenario 4: Custom timeout short/long"
    tmux send-keys -t "${session}:1.${shell_pane}" "tail -f /dev/null" Enter
    sleep 0.2
    output=$(probe_output "$session" "$shell_pane" --probe-timeout=100)
    log_assert_valid_json "$output" "short timeout probe returns valid JSON"
    log_assert_eq "$(get_last_exit_code)" "2" "short timeout exit code is 2"
    log_assert_json_eq "$output" ".probes[0].probe_details.latency_ms" "100" "short timeout latency matches"

    output=$(probe_output "$session" "$shell_pane" --probe-timeout=10000)
    log_assert_valid_json "$output" "long timeout probe returns valid JSON"
    log_assert_eq "$(get_last_exit_code)" "2" "long timeout exit code is 2"
    log_assert_json_eq "$output" ".probes[0].probe_details.latency_ms" "10000" "long timeout latency matches"

    log_section "Scenario 5: Aggressive mode fallback"
    output=$(probe_output "$session" "$shell_pane" --probe-aggressive --probe-timeout=200)
    log_assert_valid_json "$output" "aggressive probe returns valid JSON"
    log_assert_eq "$(get_last_exit_code)" "0" "aggressive probe exit code is 0"
    log_assert_json_eq "$output" ".probes[0].responsive" "true" "aggressive probe recovers responsiveness"
    log_assert_contains "$output" "escalated from keystroke_echo" "aggressive mode records escalation"

    log_section "Scenario 6: Invalid pane"
    output=$(probe_output "$session" "999")
    log_assert_valid_json "$output" "invalid pane returns JSON error"
    log_assert_eq "$(get_last_exit_code)" "2" "invalid pane exit code is 2"
    log_assert_json_eq "$output" ".probes[0].error_code" "PANE_NOT_FOUND" "invalid pane uses PANE_NOT_FOUND error"

    log_section "Scenario 7: Integration with diagnose"
    output=$(probe_output "$session" "$shell_pane")
    log_assert_eq "$(get_last_exit_code)" "0" "responsive probe exit code is 0"
    log_assert_json_eq "$output" ".probes[0].responsive" "true" "probe confirms responsive pane"
    log_exec ntm --robot-diagnose="$session" --diagnose-pane="$shell_pane"
    local diagnose_output
    diagnose_output=$(get_last_output)
    log_assert_valid_json "$diagnose_output" "diagnose returns valid JSON"
    local unresponsive_index
    unresponsive_index=$(echo "$diagnose_output" | jq ".panes.unresponsive | index(${shell_pane})")
    log_assert_eq "$unresponsive_index" "null" "diagnose does not mark responsive pane as unresponsive"

    log_summary
}

main "$@"
