package sqlutil

import (
	"strings"
	"unicode"
)

// SplitStatements splits SQL text into executable statements while respecting
// string literals, quoted identifiers, comments, and PostgreSQL dollar-quoting.
func SplitStatements(sqlText string, engine string) []string {
	var statements []string
	var cur strings.Builder

	n := len(sqlText)
	i := 0
	isMySQL := engine == "mysql"
	isPG := engine == "postgres"
	isMSSQL := engine == "sqlserver"

	flush := func() {
		stmt := strings.TrimSpace(cur.String())
		cur.Reset()
		if stmt != "" {
			statements = append(statements, stmt)
		}
	}

	for i < n {
		ch := sqlText[i]

		// Line comment: -- ...
		if ch == '-' && i+1 < n && sqlText[i+1] == '-' {
			start := i
			i += 2
			for i < n && sqlText[i] != '\n' {
				i++
			}
			cur.WriteString(sqlText[start:i])
			continue
		}

		// MySQL # line comment
		if isMySQL && ch == '#' {
			start := i
			i++
			for i < n && sqlText[i] != '\n' {
				i++
			}
			cur.WriteString(sqlText[start:i])
			continue
		}

		// Block comment: /* ... */
		if ch == '/' && i+1 < n && sqlText[i+1] == '*' {
			start := i
			i += 2
			for i < n {
				if sqlText[i] == '*' && i+1 < n && sqlText[i+1] == '/' {
					i += 2
					break
				}
				i++
			}
			cur.WriteString(sqlText[start:i])
			continue
		}

		// Single-quoted string literal
		if ch == '\'' {
			start := i
			i++
			for i < n {
				if isMySQL && sqlText[i] == '\\' && i+1 < n {
					i += 2
					continue
				}
				if sqlText[i] == '\'' {
					if i+1 < n && sqlText[i+1] == '\'' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			cur.WriteString(sqlText[start:i])
			continue
		}

		// Double-quoted identifier
		if ch == '"' {
			start := i
			i++
			for i < n {
				if sqlText[i] == '"' {
					if i+1 < n && sqlText[i+1] == '"' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			cur.WriteString(sqlText[start:i])
			continue
		}

		// Backtick identifier (MySQL / SQLite)
		if ch == '`' {
			start := i
			i++
			for i < n {
				if sqlText[i] == '`' {
					if i+1 < n && sqlText[i+1] == '`' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			cur.WriteString(sqlText[start:i])
			continue
		}

		// Bracket identifier (SQL Server)
		if isMSSQL && ch == '[' {
			start := i
			i++
			for i < n {
				if sqlText[i] == ']' {
					if i+1 < n && sqlText[i+1] == ']' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			cur.WriteString(sqlText[start:i])
			continue
		}

		// Dollar-quoted literal (PostgreSQL)
		if isPG && ch == '$' {
			if tag, tagLen, ok := parseDollarTag(sqlText[i:]); ok {
				start := i
				i += tagLen
				endIdx := strings.Index(sqlText[i:], tag)
				if endIdx >= 0 {
					i += endIdx + tagLen
				} else {
					i = n
				}
				cur.WriteString(sqlText[start:i])
				continue
			}
		}

		if ch == ';' {
			flush()
			i++
			continue
		}

		cur.WriteByte(ch)
		i++
	}

	flush()
	return filterCommentOnlyStatements(statements, isMySQL)
}

func parseDollarTag(s string) (string, int, bool) {
	if len(s) < 2 || s[0] != '$' {
		return "", 0, false
	}
	for j := 1; j < len(s); j++ {
		c := rune(s[j])
		if s[j] == '$' {
			return s[:j+1], j + 1, true
		}
		if j == 1 && !unicode.IsLetter(c) && c != '_' {
			return "", 0, false
		}
		if !unicode.IsLetter(c) && !unicode.IsDigit(c) && c != '_' {
			return "", 0, false
		}
	}
	return "", 0, false
}

func filterCommentOnlyStatements(stmts []string, isMySQL bool) []string {
	var out []string
	for _, s := range stmts {
		if hasExecutableTokens(s, isMySQL) {
			out = append(out, s)
		}
	}
	return out
}

func hasExecutableTokens(s string, isMySQL bool) bool {
	i := 0
	n := len(s)
	for i < n {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			i++
			continue
		}
		if c == '-' && i+1 < n && s[i+1] == '-' {
			for i < n && s[i] != '\n' {
				i++
			}
			continue
		}
		if isMySQL && c == '#' {
			for i < n && s[i] != '\n' {
				i++
			}
			continue
		}
		if c == '/' && i+1 < n && s[i+1] == '*' {
			i += 2
			for i < n {
				if s[i] == '*' && i+1 < n && s[i+1] == '/' {
					i += 2
					break
				}
				i++
			}
			continue
		}
		return true
	}
	return false
}

// IsReadLikeQuery returns true when the leading keyword is SELECT/SHOW/PRAGMA/EXPLAIN/WITH/DESCRIBE/DESC/VALUES
// or a DML statement containing a RETURNING clause.
func IsReadLikeQuery(stmt string, isMySQL bool) bool {
	trimmed := stripLeadingComments(stmt, isMySQL)
	if trimmed == "" {
		return false
	}
	var word strings.Builder
	for i := 0; i < len(trimmed); i++ {
		c := trimmed[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' {
			word.WriteByte(c)
		} else {
			break
		}
	}
	switch strings.ToUpper(word.String()) {
	case "SELECT", "SHOW", "PRAGMA", "EXPLAIN", "WITH", "DESCRIBE", "DESC", "VALUES":
		return true
	case "INSERT", "UPDATE", "DELETE":
		return containsKeywordOutsideLiterals(trimmed, "RETURNING", isMySQL)
	default:
		return false
	}
}

func containsKeywordOutsideLiterals(sqlText, keyword string, isMySQL bool) bool {
	n := len(sqlText)
	kwLen := len(keyword)
	i := 0
	for i < n {
		c := sqlText[i]
		if c == '\'' {
			i++
			for i < n {
				if isMySQL && sqlText[i] == '\\' && i+1 < n {
					i += 2
					continue
				}
				if sqlText[i] == '\'' {
					if i+1 < n && sqlText[i+1] == '\'' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			continue
		}
		if c == '"' || c == '`' {
			quote := c
			i++
			for i < n {
				if sqlText[i] == quote {
					if i+1 < n && sqlText[i+1] == quote {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			continue
		}
		if c == '-' && i+1 < n && sqlText[i+1] == '-' {
			for i < n && sqlText[i] != '\n' {
				i++
			}
			continue
		}
		if c == '/' && i+1 < n && sqlText[i+1] == '*' {
			i += 2
			for i < n {
				if sqlText[i] == '*' && i+1 < n && sqlText[i+1] == '/' {
					i += 2
					break
				}
				i++
			}
			continue
		}
		if i+kwLen <= n && strings.EqualFold(sqlText[i:i+kwLen], keyword) {
			prevOK := i == 0 || !isIdentChar(sqlText[i-1])
			nextOK := i+kwLen == n || !isIdentChar(sqlText[i+kwLen])
			if prevOK && nextOK {
				return true
			}
		}
		i++
	}
	return false
}

func isIdentChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

func stripLeadingComments(s string, isMySQL bool) string {
	i := 0
	n := len(s)
	for i < n {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			i++
			continue
		}
		if c == '-' && i+1 < n && s[i+1] == '-' {
			for i < n && s[i] != '\n' {
				i++
			}
			continue
		}
		if isMySQL && c == '#' {
			for i < n && s[i] != '\n' {
				i++
			}
			continue
		}
		if c == '/' && i+1 < n && s[i+1] == '*' {
			i += 2
			for i < n {
				if s[i] == '*' && i+1 < n && s[i+1] == '/' {
					i += 2
					break
				}
				i++
			}
			continue
		}
		break
	}
	return s[i:]
}
