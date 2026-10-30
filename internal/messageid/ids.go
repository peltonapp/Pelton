// Package messageid validates message identifiers and prepares reply ancestry.
package messageid

import "strings"

const (
	maxReferences     = 20
	maxReferencesLine = 998
	minKeptNewest     = 3
)

// Normalize accepts a bare ID or exactly one enclosing angle-bracket pair and
// returns the bare ID unchanged. Empty, malformed, non-ASCII, whitespace-bearing,
// or oversized IDs return "", false. A bracketed ID may be at most 980 bytes.
func Normalize(value string) (bare string, ok bool) {
	if strings.HasPrefix(value, "<") && strings.HasSuffix(value, ">") {
		value = value[1 : len(value)-1]
	}
	if len(value) == 0 || len(value) > 978 {
		return "", false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c <= 32 || c >= 127 || c == '<' || c == '>' {
			return "", false
		}
	}
	left, right, found := strings.Cut(value, "@")
	if !found || strings.Contains(right, "@") || !dotAtom(left) {
		return "", false
	}
	if strings.HasPrefix(right, "[") && strings.HasSuffix(right, "]") {
		for i := 1; i < len(right)-1; i++ {
			if right[i] == '[' || right[i] == ']' || right[i] == '\\' {
				return "", false
			}
		}
	} else if !dotAtom(right) {
		return "", false
	}
	return value, true
}

// Bracket returns a valid ID enclosed in exactly one angle-bracket pair.
// It accepts bare or bracketed input; empty or invalid input returns "", false.
func Bracket(value string) (token string, ok bool) {
	bare, ok := Normalize(value)
	if !ok {
		return "", false
	}
	return "<" + bare + ">", true
}

// References returns a fresh, non-nil slice of bare IDs, with invalid ancestors
// removed, exact duplicates collapsed, and the valid parent moved to the end.
// A missing or invalid parent returns an empty slice. Chains keep the root and
// 19 newest IDs, then drop the oldest after root until the unfolded header fits
// 998 bytes or only root and three newest IDs remain, even if still oversized.
// Chains of four or fewer IDs are never trimmed for length. Inputs are unchanged.
func References(ancestors []string, parent string) []string {
	result := []string{}
	parent, ok := Normalize(parent)
	if !ok {
		return result
	}
	seen := make(map[string]bool)
	for _, value := range ancestors {
		bare, ok := Normalize(value)
		if !ok || bare == parent || seen[bare] {
			continue
		}
		seen[bare] = true
		result = append(result, bare)
	}
	result = append(result, parent)
	if len(result) > maxReferences {
		copy(result[1:], result[len(result)-(maxReferences-1):])
		result = result[:maxReferences]
	}
	length := len("References: ") + len(result) - 1
	for _, id := range result {
		length += len(id) + 2
	}
	for length > maxReferencesLine && len(result) > 1+minKeptNewest {
		length -= len(result[1]) + 3
		copy(result[1:], result[2:])
		result = result[:len(result)-1]
	}
	return result
}

func dotAtom(value string) bool {
	if value == "" || value[0] == '.' || value[len(value)-1] == '.' {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c == '.' {
			if value[i-1] == '.' {
				return false
			}
			continue
		}
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-/=?^_`{|}~", rune(c)) {
			continue
		}
		return false
	}
	return true
}
