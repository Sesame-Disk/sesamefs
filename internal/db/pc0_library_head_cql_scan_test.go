package db

import (
	"regexp"
	"strings"
	"testing"
)

// CQL table/IF folding for the HEAD SERIAL-domain inventory matches R12
// (internal/integration/r12_serial_domain_guard_test.go). That scanner is
// behind //go:build integration and targets blocks/orphans, so the folding
// is duplicated here rather than imported. A name-literal regex that
// requires `DELETE FROM libraries` then `IF head_commit_id` as the first
// predicate is a false green: `IF created_at = ? AND head_commit_id`,
// `sesamefs.libraries`, and quoted identifiers would leave discovery.
const pc0CQLIdentifierPattern = `(?:"(?:[^"]|"")+"|[A-Za-z_][A-Za-z0-9_]*)`

var pc0DeleteFromPattern = regexp.MustCompile(
	`(?is)\bDELETE(?:\s+[\w"',\[\]\s.]+?)?\s+FROM\s+(` +
		pc0CQLIdentifierPattern + `)(?:\s*\.\s*(` + pc0CQLIdentifierPattern + `))?`)

var pc0UpdatePattern = regexp.MustCompile(
	`(?is)\bUPDATE\s+(` + pc0CQLIdentifierPattern + `)(?:\s*\.\s*(` + pc0CQLIdentifierPattern + `))?`)

var pc0IFKeywordPattern = regexp.MustCompile(`(?i)\bIF\b`)

var pc0HeadCommitIDColumnPattern = regexp.MustCompile(`(?i)(?:"head_commit_id"|\bhead_commit_id\b)`)

var pc0IFExistsOnlyPattern = regexp.MustCompile(`(?is)^(?:NOT\s+)?EXISTS\b`)

func pc0PreparedCQL(query string) string {
	return pc0StripCQLStringLiterals(pc0StripCQLComments(query))
}

func pc0CQLMatchTable(matches []string) string {
	if len(matches) >= 3 && matches[2] != "" {
		return pc0NormalizeCQLIdentifier(matches[2])
	}
	if len(matches) >= 2 {
		return pc0NormalizeCQLIdentifier(matches[1])
	}
	return ""
}

func pc0NormalizeCQLIdentifier(identifier string) string {
	identifier = strings.TrimSpace(identifier)
	if len(identifier) >= 2 && strings.HasPrefix(identifier, `"`) && strings.HasSuffix(identifier, `"`) {
		return strings.ReplaceAll(identifier[1:len(identifier)-1], `""`, `"`)
	}
	return strings.ToLower(identifier)
}

func pc0CQLIFNamesHeadCommitID(fragment string) bool {
	loc := pc0IFKeywordPattern.FindStringIndex(fragment)
	if loc == nil {
		return false
	}
	rest := strings.TrimSpace(fragment[loc[1]:])
	if pc0IFExistsOnlyPattern.MatchString(rest) && !pc0HeadCommitIDColumnPattern.MatchString(rest) {
		return false
	}
	return pc0HeadCommitIDColumnPattern.MatchString(rest)
}

// pc0CQLIsLibrariesHeadIFDelete reports whether query contains a conditional
// DELETE of the libraries relation whose IF clause names head_commit_id
// (any predicate position, qualified/quoted table spellings included).
func pc0CQLSubmatchTable(prepared string, loc []int) string {
	matches := make([]string, 3)
	matches[0] = prepared[loc[0]:loc[1]]
	if len(loc) > 3 && loc[2] >= 0 {
		matches[1] = prepared[loc[2]:loc[3]]
	}
	if len(loc) > 5 && loc[4] >= 0 {
		matches[2] = prepared[loc[4]:loc[5]]
	}
	return pc0CQLMatchTable(matches)
}

func pc0CQLIsLibrariesHeadIFDelete(query string) bool {
	prepared := pc0PreparedCQL(query)
	for _, loc := range pc0DeleteFromPattern.FindAllStringSubmatchIndex(prepared, -1) {
		if pc0CQLSubmatchTable(prepared, loc) != "libraries" {
			continue
		}
		if pc0CQLIFNamesHeadCommitID(prepared[loc[0]:]) {
			return true
		}
	}
	return false
}

// pc0CQLCompetesForLibraryHead reports whether query is an UPDATE or DELETE
// of libraries whose IF clause names head_commit_id. Used to pin the
// MapScanCAS chain of inventoried HEAD-authority LWTs.
func pc0CQLCompetesForLibraryHead(query string) bool {
	if pc0CQLIsLibrariesHeadIFDelete(query) {
		return true
	}
	prepared := pc0PreparedCQL(query)
	for _, loc := range pc0UpdatePattern.FindAllStringSubmatchIndex(prepared, -1) {
		if pc0CQLSubmatchTable(prepared, loc) != "libraries" {
			continue
		}
		if pc0CQLIFNamesHeadCommitID(prepared[loc[0]:]) {
			return true
		}
	}
	return false
}

func pc0StripCQLStringLiterals(query string) string {
	var out strings.Builder
	out.Grow(len(query))
	inString := false
	for index := 0; index < len(query); index++ {
		char := query[index]
		if !inString {
			out.WriteByte(char)
			if char == '\'' {
				inString = true
			}
			continue
		}
		if char == '\'' {
			if index+1 < len(query) && query[index+1] == '\'' {
				out.WriteString("  ")
				index++
				continue
			}
			out.WriteByte(char)
			inString = false
			continue
		}
		if char == '\r' || char == '\n' {
			out.WriteByte(char)
			continue
		}
		out.WriteByte(' ')
	}
	return out.String()
}

func pc0StripCQLComments(query string) string {
	var out strings.Builder
	out.Grow(len(query))
	inSingleQuote := false
	inDoubleQuote := false
	for index := 0; index < len(query); {
		char := query[index]
		if inSingleQuote {
			out.WriteByte(char)
			index++
			if char == '\'' {
				if index < len(query) && query[index] == '\'' {
					out.WriteByte(query[index])
					index++
					continue
				}
				inSingleQuote = false
			}
			continue
		}
		if inDoubleQuote {
			out.WriteByte(char)
			index++
			if char == '"' {
				if index < len(query) && query[index] == '"' {
					out.WriteByte(query[index])
					index++
					continue
				}
				inDoubleQuote = false
			}
			continue
		}
		if char == '\'' {
			inSingleQuote = true
			out.WriteByte(char)
			index++
			continue
		}
		if char == '"' {
			inDoubleQuote = true
			out.WriteByte(char)
			index++
			continue
		}
		if char == '-' && index+1 < len(query) && query[index+1] == '-' {
			out.WriteString("  ")
			index += 2
			for index < len(query) && query[index] != '\r' && query[index] != '\n' {
				index++
			}
			continue
		}
		if char == '/' && index+1 < len(query) && query[index+1] == '*' {
			out.WriteString("  ")
			index += 2
			for index < len(query) {
				if query[index] == '*' && index+1 < len(query) && query[index+1] == '/' {
					out.WriteString("  ")
					index += 2
					break
				}
				if query[index] == '\r' || query[index] == '\n' {
					out.WriteByte(query[index])
				} else {
					out.WriteByte(' ')
				}
				index++
			}
			continue
		}
		out.WriteByte(char)
		index++
	}
	return out.String()
}

func TestPC0HeadAuthorityDeleteCQLRecognition(t *testing.T) {
	// The #221-era name-literal regex: IF must be the first predicate and the
	// table must be the bare identifier libraries. Kept as a witness that the
	// forms below used to be false greens.
	old := regexp.MustCompile(`(?is)\bdelete\s+from\s+libraries\b[^;]*?\bif\s+head_commit_id\b`)

	cases := []struct {
		name    string
		cql     string
		want    bool
		oldMiss bool
	}{
		{
			name: "production rollback",
			cql:  "DELETE FROM libraries WHERE org_id = ? AND library_id = ? IF head_commit_id = null",
			want: true,
		},
		{
			name:    "IF another column then head_commit_id",
			cql:     "DELETE FROM libraries WHERE org_id = ? AND library_id = ? IF created_at = ? AND head_commit_id = null",
			want:    true,
			oldMiss: true,
		},
		{
			name:    "keyspace-qualified table",
			cql:     "DELETE FROM sesamefs.libraries WHERE org_id = ? AND library_id = ? IF head_commit_id = null",
			want:    true,
			oldMiss: true,
		},
		{
			name:    "quoted qualified table",
			cql:     `DELETE FROM "sesamefs"."libraries" WHERE org_id = ? AND library_id = ? IF head_commit_id = null`,
			want:    true,
			oldMiss: true,
		},
		{
			name:    "quoted table identifier",
			cql:     `DELETE FROM "libraries" WHERE org_id = ? AND library_id = ? IF head_commit_id = null`,
			want:    true,
			oldMiss: true,
		},
		{
			name:    "quoted head_commit_id column",
			cql:     `DELETE FROM libraries WHERE org_id = ? AND library_id = ? IF "head_commit_id" = null`,
			want:    true,
			oldMiss: true,
		},
		{
			name:    "cell delete of head_commit_id",
			cql:     "DELETE head_commit_id FROM libraries WHERE org_id = ? AND library_id = ? IF head_commit_id = null",
			want:    true,
			oldMiss: true,
		},
		{
			name: "unconditional delete is not a HEAD guard",
			cql:  "DELETE FROM libraries WHERE org_id = ? AND library_id = ?",
			want: false,
		},
		{
			name: "IF EXISTS is not a HEAD guard",
			cql:  "DELETE FROM libraries WHERE org_id = ? AND library_id = ? IF EXISTS",
			want: false,
		},
		{
			name: "libraries_by_id is not the canonical relation",
			cql:  "DELETE FROM libraries_by_id WHERE library_id = ? IF head_commit_id = null",
			want: false,
		},
		{
			name: "IF only inside a string value",
			cql:  "DELETE FROM libraries WHERE name = 'IF head_commit_id = null'",
			want: false,
		},
		{
			name: "UPDATE is not a DELETE guard",
			cql:  "UPDATE libraries SET name = ? WHERE org_id = ? AND library_id = ? IF head_commit_id = ?",
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pc0CQLIsLibrariesHeadIFDelete(tc.cql)
			if got != tc.want {
				t.Fatalf("pc0CQLIsLibrariesHeadIFDelete(%q) = %v, want %v", tc.cql, got, tc.want)
			}
			if tc.oldMiss && old.MatchString(tc.cql) {
				t.Fatalf("witness regex unexpectedly matches %q; the oldMiss classification is stale", tc.cql)
			}
			if tc.want && !tc.oldMiss && !old.MatchString(tc.cql) {
				t.Fatalf("production-shaped CQL %q must still match the old regex", tc.cql)
			}
		})
	}
}

func TestPC0CQLCompetesForLibraryHeadRecognizesQualifiedUpdate(t *testing.T) {
	if !pc0CQLCompetesForLibraryHead("UPDATE sesamefs.libraries SET head_commit_id = ? WHERE org_id = ? AND library_id = ? IF created_at != null AND head_commit_id = ?") {
		t.Fatal("qualified UPDATE with head_commit_id not as the first IF predicate must still compete for HEAD")
	}
	if pc0CQLCompetesForLibraryHead("UPDATE libraries SET name = ? WHERE org_id = ? AND library_id = ?") {
		t.Fatal("unconditional UPDATE of libraries must not be classified as a HEAD LWT")
	}
}
