package pipeline

type scopedValue struct {
	value  interface{}
	exists bool
}

// VariableScope records the previous values for a narrow set of variables so
// loop and branch bodies can shadow them and then restore the outer context.
type VariableScope struct {
	values map[string]scopedValue
}

// CaptureVariableScope snapshots only the requested keys. Callers are
// responsible for holding the appropriate variable lock.
func CaptureVariableScope(vars map[string]interface{}, keys ...string) VariableScope {
	scope := VariableScope{values: make(map[string]scopedValue, len(keys))}
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		value, exists := vars[key]
		scope.values[key] = scopedValue{value: value, exists: exists}
	}
	return scope
}

// Restore puts a previously captured scope back into vars. Callers are
// responsible for holding the appropriate variable lock.
func (s VariableScope) Restore(vars map[string]interface{}) {
	for key, prev := range s.values {
		if prev.exists {
			vars[key] = prev.value
		} else {
			delete(vars, key)
		}
	}
}

func loopScopeKeys(varName string) []string {
	keys := []string{
		"loop." + varName,
		"loop.item",
		"loop.index",
		"loop.count",
		"loop.first",
		"loop.last",
	}
	return dedupeScopeKeys(keys)
}

func dedupeScopeKeys(keys []string) []string {
	seen := make(map[string]struct{}, len(keys))
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}

func captureAllVariables(vars map[string]interface{}) map[string]interface{} {
	if vars == nil {
		return nil
	}
	snapshot := make(map[string]interface{}, len(vars))
	for k, v := range vars {
		snapshot[k] = v
	}
	return snapshot
}

func restoreAllVariables(state *ExecutionState, snapshot map[string]interface{}) {
	if snapshot == nil {
		state.Variables = nil
		return
	}
	state.Variables = captureAllVariables(snapshot)
}
