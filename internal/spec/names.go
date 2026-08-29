package spec

import (
	"fmt"
	"strings"
)

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

// reservedCodeNames are symbols the embedded gorm.Model already occupies on
// every generated model, so a field may not mint them.
//
// This is the minimum needed to keep M0 generation from emitting a struct
// with duplicate fields. The complete reservation table the allocator
// consults — framework helpers, generated hook names, per-namespace
// reservations — is F0 work (ADR-001 §12).
var reservedCodeNames = map[string]struct{}{
	"ID": {}, "CreatedAt": {}, "UpdatedAt": {}, "DeletedAt": {},
}

// reservedStorageNames mirrors reservedCodeNames on the database side; it
// matches the set Gombit's own resourcegen refuses.
var reservedStorageNames = map[string]struct{}{
	"id": {}, "created_at": {}, "updated_at": {}, "deleted_at": {},
}

// IsReservedCodeName reports whether a generated symbol collides with the
// embedded gorm.Model.
func IsReservedCodeName(name string) bool {
	_, found := reservedCodeNames[name]
	return found
}

// IsReservedStorageName reports whether a column name collides with the
// embedded gorm.Model.
func IsReservedStorageName(name string) bool {
	_, found := reservedStorageNames[name]
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

// isDecimalLiteral reports whether value is a plain decimal number.
//
// This is deliberately narrower than strconv.ParseFloat, which also accepts
// "NaN", "Inf", hexadecimal floats such as "0x1p-2" and exponent notation.
// A default is emitted verbatim into the generated model and the migration,
// so the grammar is restricted to what a column default can actually hold:
// an optional sign, decimal digits, and at most one fractional point.
func isDecimalLiteral(value string) bool {
	if value == "" {
		return false
	}

	index := 0
	if value[0] == '+' || value[0] == '-' {
		index++
	}

	digits := 0
	seenPoint := false
	for ; index < len(value); index++ {
		switch char := value[index]; {
		case char >= '0' && char <= '9':
			digits++
		case char == '.':
			if seenPoint {
				return false
			}
			seenPoint = true
		default:
			return false
		}
	}
	return digits > 0
}

// IsSafeDefaultValue reports whether a string-valued default can be carried
// into generated output, and if not, the first offending rune.
//
// The compiler emits defaults inside a GORM struct tag. Empirically, neither
// Go's struct-tag unquoting nor GORM's ';'-separated tag grammar can
// round-trip a value containing ';', '"', a backtick, a backslash, or a
// control character: the tag either fails to unquote or is truncated at the
// separator. A default that cannot be emitted is not a usable default, so the
// validator rejects it here — keeping "spec-valid" in step with "emittable"
// (the generator relies on this rather than re-deriving it).
func IsSafeDefaultValue(value string) (ok bool, bad string) {
	for _, r := range value {
		switch {
		case r == ';', r == '"', r == '`', r == '\\':
			return false, string(r)
		case r < 0x20: // control characters, including newline and tab
			return false, fmt.Sprintf("\\x%02x", r)
		}
	}
	return true, ""
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
