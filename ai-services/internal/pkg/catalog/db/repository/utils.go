package repository

import "strings"

// escapeLikePattern escapes SQL LIKE/ILIKE wildcard characters in user-supplied
// search input so that '%', '_', and '\' are treated as literals.
//
// PostgreSQL's ILIKE operator does not escape wildcards automatically — a user
// typing "%" would match every row without this guard. We pair this with
// ESCAPE '\' in the query so that the backslash acts as the escape character.
//
// Note: ILIKE alone handles case-insensitivity; the ESCAPE clause is an
// additional safeguard specifically for wildcard injection, which ILIKE does
// not address on its own.
func escapeLikePattern(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)

	return s
}
