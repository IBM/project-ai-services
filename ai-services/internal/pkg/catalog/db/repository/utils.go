package repository

import (
	"fmt"
	"strings"
)

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

// buildSearchClause builds an OR-joined ILIKE clause across the given columns
// for the search term, appending the bound parameter to args.
// Each column produces: col ILIKE $N ESCAPE '\'
// Multiple columns are joined with OR so the term matches any of them.
// To extend search to a new column, simply add it to the caller's column list.
func buildSearchClause(columns []string, args *[]interface{}, term string) string {
	pattern := "%" + escapeLikePattern(term) + "%"
	*args = append(*args, pattern)
	idx := len(*args)

	clauses := make([]string, len(columns))
	for i, col := range columns {
		clauses[i] = fmt.Sprintf("%s ILIKE $%d ESCAPE '\\'", col, idx)
	}

	return "(" + strings.Join(clauses, " OR ") + ")"
}
