package spec

import "strings"

// goKeywords are reserved words that can never be used as a generated symbol.
var goKeywords = map[string]struct{}{
	"break": {}, "case": {}, "chan": {}, "const": {}, "continue": {},
	"default": {}, "defer": {}, "else": {}, "fallthrough": {}, "for": {},
	"func": {}, "go": {}, "goto": {}, "if": {}, "import": {},
	"interface": {}, "map": {}, "package": {}, "range": {}, "return": {},
	"select": {}, "struct": {}, "switch": {}, "type": {}, "var": {},
}

// IsGoKeyword reports whether name is a Go reserved word.
func IsGoKeyword(name string) bool {
	_, found := goKeywords[name]
	return found
}

// IsExportedGoIdent reports whether name is a legal exported Go identifier,
// which is what every generated code symbol must be (ADR-001 §6).
func IsExportedGoIdent(name string) bool {
	if name == "" || IsGoKeyword(name) {
		return false
	}
	for index, char := range name {
		switch {
		case char >= 'A' && char <= 'Z':
		case char >= 'a' && char <= 'z':
			if index == 0 {
				return false // must start uppercase to be exported
			}
		case char >= '0' && char <= '9':
			if index == 0 {
				return false // may not start with a digit
			}
		case char == '_':
			if index == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// IsStorageName reports whether name is a legal lower_snake_case storage
// identifier suitable for a table or column name.
//
// Go keywords are deliberately allowed: storage names map to the database and
// are a separate naming domain from code symbols (ADR-001 D2), so a column
// may legitimately be called "type".
func IsStorageName(name string) bool {
	if name == "" || len(name) > 63 {
		return false
	}
	if name[0] == '_' || strings.HasSuffix(name, "_") || strings.Contains(name, "__") {
		return false
	}
	for index, char := range name {
		switch {
		case char >= 'a' && char <= 'z':
		case char == '_':
		case char >= '0' && char <= '9':
			if index == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// IsSlug reports whether value is a legal lower kebab-case URL slug.
func IsSlug(value string) bool {
	if value == "" || len(value) > 63 {
		return false
	}
	if strings.HasPrefix(value, "-") || strings.HasSuffix(value, "-") || strings.Contains(value, "--") {
		return false
	}
	for index, char := range value {
		switch {
		case char >= 'a' && char <= 'z':
		case char == '-':
		case char >= '0' && char <= '9':
			if index == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}
