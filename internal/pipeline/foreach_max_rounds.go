package pipeline

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// roundOverridesCtxKey scopes per-iteration round/rounds_remaining bindings
// onto a context.Context so parallel foreach iterations can each carry their
// own values without racing on shared state.Variables (bd-2ubxp.20).
type roundOverridesCtxKey struct{}

// withRoundOverrides returns a derived context that exposes the supplied
// round-binding overlay to substitution call sites. Pass nil to clear.
func withRoundOverrides(ctx context.Context, overrides map[string]interface{}) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, roundOverridesCtxKey{}, overrides)
}

// roundOverridesFromCtx returns the round-binding overlay attached to ctx, or
// nil if none. The map should be treated as read-only by callers.
func roundOverridesFromCtx(ctx context.Context) map[string]interface{} {
	if ctx == nil {
		return nil
	}
	v, _ := ctx.Value(roundOverridesCtxKey{}).(map[string]interface{})
	return v
}

// buildRoundOverrides constructs the overlay map for a single round of a
// foreach iteration. Keys mirror the historical pushRoundVars bindings:
// `round` / `rounds_remaining` (top-level shortcuts) and `loop.round` /
// `loop.rounds_remaining` (loop-namespaced form). Values are int so the
// substitutor's formatValue handles printing identically to the prior path.
func buildRoundOverrides(round, maxRounds int) map[string]interface{} {
	rem := maxRounds - round
	return map[string]interface{}{
		"round":                 round,
		"rounds_remaining":      rem,
		"loop.round":            round,
		"loop.rounds_remaining": rem,
	}
}

// resolveForeachMaxRounds returns the resolved max_rounds for a foreach step.
// Returns 1 when MaxRounds is unset (single round, the historical default
// behavior). An explicit literal or expression that fails to resolve to a
// positive integer returns an error so the iteration fails closed (bd-2ubxp.14).
//
// The expression form ("${defaults.hard_caps.foo}", "${vars.cap}", etc.) is
// resolved against the executor's substitutor with workflow defaults applied,
// matching LoopExecutor.resolveIntOrExpr's contract for max_iterations.
func (e *Executor) resolveForeachMaxRounds(parent *Step) (int, error) {
	fc := parent.Foreach
	if fc == nil {
		fc = parent.ForeachPane
	}
	if fc == nil {
		return 1, nil
	}
	mr := fc.MaxRounds
	if mr.Expr == "" && mr.Value <= 0 {
		return 1, nil
	}
	if mr.Expr == "" {
		return mr.Value, nil
	}

	e.varMu.RLock()
	e.stateMu.RLock()
	workflowID := ""
	if e.state != nil {
		workflowID = e.state.WorkflowID
	}
	sub := NewSubstitutor(e.state, e.config.Session, workflowID)
	sub.SetDefaults(e.defaults)
	sub.SetMaxDepth(e.limits.MaxSubstitutionDepth)
	resolved, subErr := sub.SubstituteStrict(e.substituteRuntimeVariables(mr.Expr))
	e.stateMu.RUnlock()
	e.varMu.RUnlock()

	if subErr != nil {
		return 0, fmt.Errorf("resolve max_rounds expression %q: %w", mr.Expr, subErr)
	}
	parsed, parseErr := strconv.Atoi(strings.TrimSpace(resolved))
	if parseErr != nil {
		return 0, fmt.Errorf("resolve max_rounds expression %q: parse %q as integer: %w", mr.Expr, resolved, parseErr)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("resolve max_rounds expression %q: value %d must be > 0", mr.Expr, parsed)
	}
	return parsed, nil
}

// Per-iteration round bindings are no longer written to state.Variables.
// Instead, the round loop in executeForeachIteration derives a child ctx
// via withRoundOverrides; substitution helpers consult that ctx and pass
// the overlay to Substitutor.SetLocalOverrides. This keeps parallel
// iterations from racing on shared state.Variables["round"] (bd-2ubxp.20).

// rewriteRoundStepIDs deep-clones an iteration's body steps and suffixes each
// step's ID with `_round<N>` so per-round results land under unique keys in
// state.Steps. Without this, last-writer-wins erases earlier rounds' results
// from state.Steps even though iterResult.Results preserves order.
func rewriteRoundStepIDs(steps []Step, round int) []Step {
	if len(steps) == 0 {
		return steps
	}
	out := make([]Step, len(steps))
	for i := range steps {
		out[i] = steps[i]
		if out[i].ID != "" {
			out[i].ID = fmt.Sprintf("%s_round%d", out[i].ID, round)
		}
	}
	return out
}
