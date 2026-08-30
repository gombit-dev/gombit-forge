package spec

import "strings"

// Normalize converts a human label into a candidate exported Go identifier
// (ADR-001 §6). It is the first step of symbol minting (§5): the candidate it
// returns is then checked against the symbol ledger and the reserved-name table
// and disambiguated before it is frozen. Normalize itself performs none of
// those checks — it does not consult the ledger or the reserved set, so
// "created at" normalizes to "CreatedAt" here even though that symbol is
// reserved; catching that collision is minting's job (#17/#18), not this
// function's.
//
// The rules are fixed and deterministic — the same label yields the same
// identifier on every run and every build — because a code symbol is a frozen
// source ABI and must not shift under a project as the toolchain moves. If the
// rules ever change they change behind an explicit version, never silently.
//
// The transformation:
//
//   - fold Latin-1 accented letters to their ASCII base (café → Cafe, Straße →
//     Strasse); any other non-ASCII rune is treated as a separator;
//   - split the label into words on any non-alphanumeric run and at each
//     lowercase→uppercase boundary, so both "created-at" and "createdAt" yield
//     ["created","at"];
//   - upper-case the first letter of each word and concatenate (PascalCase),
//     preserving the rest of each word so acronyms survive (API → API);
//   - if the result would start with a digit, replace that leading digit with
//     its English word so the identifier is legal ("2 factor" → TwoFactor).
//
// ok is false when the label has no usable alphanumeric content and therefore
// cannot form an identifier (empty, whitespace, all punctuation, or entirely
// non-foldable symbols). Callers must handle that case rather than assume a
// name; the minting pipeline falls back to a kind-derived symbol.
func Normalize(label string) (ident string, ok bool) {
	var folded strings.Builder
	folded.Grow(len(label))
	for _, r := range label {
		folded.WriteString(foldToASCII(r))
	}

	words := splitWords(folded.String())
	if len(words) == 0 {
		return "", false
	}

	var b strings.Builder
	for _, w := range words {
		b.WriteString(upperFirstASCII(w))
	}
	out := b.String()

	// A Go identifier may not begin with a digit. Once the leading digit is a
	// word, any later digits are legal, so only the first rune needs spelling.
	if d := out[0]; d >= '0' && d <= '9' {
		out = digitWords[d-'0'] + out[1:]
	}

	// Constructed to be valid; verify rather than trust, so a future rule change
	// that breaks the invariant fails a test instead of emitting bad source.
	if !IsExportedGoIdent(out) {
		return "", false
	}
	return out, true
}

// digitWords spells single decimal digits for leading-digit repair.
var digitWords = [10]string{
	"Zero", "One", "Two", "Three", "Four", "Five", "Six", "Seven", "Eight", "Nine",
}

// splitWords breaks an ASCII string into identifier words. A word is a maximal
// run of ASCII alphanumerics; runs are also cut at each lowercase→uppercase
// boundary so camelCase input splits the way separator-delimited input does.
func splitWords(s string) []string {
	var words []string
	var cur []byte
	var prev byte

	flush := func() {
		if len(cur) > 0 {
			words = append(words, string(cur))
			cur = cur[:0]
		}
	}

	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			if prev >= 'a' && prev <= 'z' && c >= 'A' && c <= 'Z' {
				flush() // camelCase boundary
			}
			cur = append(cur, c)
			prev = c
		default:
			flush()
			prev = 0
		}
	}
	flush()
	return words
}

// upperFirstASCII upper-cases the first byte of an ASCII word, leaving the rest
// untouched so an all-caps acronym stays intact.
func upperFirstASCII(w string) string {
	if w == "" {
		return ""
	}
	b := []byte(w)
	if b[0] >= 'a' && b[0] <= 'z' {
		b[0] -= 'a' - 'A'
	}
	return string(b)
}

// foldToASCII maps one rune to its ASCII identifier contribution: ASCII runes
// pass through; Latin-1 accented letters fold to their base letter (some to two
// letters, e.g. Æ→AE, ß→ss); every other non-ASCII rune becomes a space, which
// splitWords treats as a separator. This covers the common European labels
// without a Unicode-normalization dependency; scripts it cannot fold degrade to
// separators rather than producing an invalid identifier.
func foldToASCII(r rune) string {
	if r < 0x80 {
		return string(r)
	}
	switch r {
	case 'À', 'Á', 'Â', 'Ã', 'Ä', 'Å':
		return "A"
	case 'Æ':
		return "AE"
	case 'Ç':
		return "C"
	case 'È', 'É', 'Ê', 'Ë':
		return "E"
	case 'Ì', 'Í', 'Î', 'Ï':
		return "I"
	case 'Ð':
		return "D"
	case 'Ñ':
		return "N"
	case 'Ò', 'Ó', 'Ô', 'Õ', 'Ö', 'Ø':
		return "O"
	case 'Ù', 'Ú', 'Û', 'Ü':
		return "U"
	case 'Ý':
		return "Y"
	case 'Þ':
		return "TH"
	case 'ß':
		return "ss"
	case 'à', 'á', 'â', 'ã', 'ä', 'å':
		return "a"
	case 'æ':
		return "ae"
	case 'ç':
		return "c"
	case 'è', 'é', 'ê', 'ë':
		return "e"
	case 'ì', 'í', 'î', 'ï':
		return "i"
	case 'ð':
		return "d"
	case 'ñ':
		return "n"
	case 'ò', 'ó', 'ô', 'õ', 'ö', 'ø':
		return "o"
	case 'ù', 'ú', 'û', 'ü':
		return "u"
	case 'ý', 'ÿ':
		return "y"
	case 'þ':
		return "th"
	default:
		return " "
	}
}
