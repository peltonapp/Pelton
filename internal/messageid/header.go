package messageid

// ParseHeader reads bracketed IDs separated by optional folding whitespace and
// nested comments, preserving bare IDs, case, order and duplicates. Empty input
// returns a non-nil empty slice and true. Closed invalid tokens are skipped and
// set valid to false; bare junk, controls, illegal folding or unbalanced lexical
// structure clear all recovered IDs and return false. IDs use Normalize's policy.
func ParseHeader(value string) (ids []string, valid bool) {
	ids = []string{}
	valid = true
	// Check the whole field before recovery so controls cannot hide in an
	// otherwise skippable token or comment. Folding stays in each intact token.
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c == '\r' {
			if i+2 >= len(value) || value[i+1] != '\n' || (value[i+2] != ' ' && value[i+2] != '\t') {
				return []string{}, false
			}
			i++
		} else if c == '\n' || c == 127 || (c < 32 && c != '\t') {
			return []string{}, false
		}
	}

	for i := 0; i < len(value); {
		switch value[i] {
		case ' ', '\t', '\r', '\n':
			i++
		case '(':
			depth := 1
			i++
			for i < len(value) && depth > 0 {
				switch value[i] {
				case '\\':
					// A quoted pair escapes one comment character, never a fold.
					if i+1 >= len(value) || value[i+1] == '\r' || value[i+1] == '\n' {
						return []string{}, false
					}
					i++
				case '(':
					depth++
				case ')':
					depth--
				}
				i++
			}
			if depth != 0 {
				return []string{}, false
			}
		case '<':
			start := i
			i++
			for i < len(value) && value[i] != '>' {
				if value[i] == '<' {
					return []string{}, false
				}
				i++
			}
			if i == len(value) {
				return []string{}, false
			}
			i++
			if id, ok := Normalize(value[start:i]); ok {
				ids = append(ids, id)
			} else {
				valid = false
			}
		default:
			return []string{}, false
		}
	}
	return ids, valid
}
