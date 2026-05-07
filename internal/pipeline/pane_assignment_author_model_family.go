package pipeline

import (
	"fmt"
	"strings"
)

// foreachAuthorModelFamily resolves the source model family for
// by_model_family_difference routing. Prefer explicit family fields when
// present, then fall back to author/model aliases.
func foreachAuthorModelFamily(item interface{}) string {
	if family := foreachItemStringNonEmpty(item, "model_family", "family", "type"); family != "" {
		return family
	}
	return foreachItemStringNonEmpty(item, "author_model", "model")
}

func foreachItemStringNonEmpty(item interface{}, keys ...string) string {
	switch v := item.(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	case map[string]interface{}:
		for _, key := range keys {
			value, ok := v[key]
			if !ok {
				continue
			}
			if s := strings.TrimSpace(fmt.Sprint(value)); s != "" {
				return s
			}
		}
	case map[string]string:
		for _, key := range keys {
			value, ok := v[key]
			if !ok {
				continue
			}
			if s := strings.TrimSpace(value); s != "" {
				return s
			}
		}
	}
	return ""
}
