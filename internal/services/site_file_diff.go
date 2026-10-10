package services

import (
	"fmt"
	"regexp"
	"strings"
)

// UnifiedSiteFileDiff is the difference the domain screen shows between the
// file on disk and the Panel's text (D-031): a unified diff with three lines
// of context, computed on the server. It is bounded: inputs longer than
// maxSiteFileDiffLines lines and output beyond maxSiteFileDiffBytes are cut and
// the second result says so. A vhost the Panel writes holds no secret, but an
// owner's line can (an Authorization header, a password), so a line whose text
// looks like a credential is shown with its value hidden.
//
// UnifiedSiteFileDiff, diskteki dosya ile Panel'in metni arasındaki farktır;
// sınırlıdır ve kimlik bilgisine benzeyen satırın değeri gizlenir.
func UnifiedSiteFileDiff(oldName, newName string, oldContent, newContent []byte) (string, bool) {
	oldLines := splitDiffLines(string(oldContent))
	newLines := splitDiffLines(string(newContent))
	truncated := false
	if len(oldLines) > maxSiteFileDiffLines {
		oldLines = oldLines[:maxSiteFileDiffLines]
		truncated = true
	}
	if len(newLines) > maxSiteFileDiffLines {
		newLines = newLines[:maxSiteFileDiffLines]
		truncated = true
	}
	ops := diffLineOps(oldLines, newLines)

	var out strings.Builder
	out.WriteString("--- " + oldName + "\n")
	out.WriteString("+++ " + newName + "\n")
	const context = 3
	for start := 0; start < len(ops); {
		// Find the next change.
		for start < len(ops) && ops[start].kind == ' ' {
			start++
		}
		if start >= len(ops) {
			break
		}
		hunkStart := start - context
		if hunkStart < 0 {
			hunkStart = 0
		}
		end := start
		for end < len(ops) {
			if ops[end].kind != ' ' {
				end++
				continue
			}
			run := end
			for run < len(ops) && ops[run].kind == ' ' {
				run++
			}
			if run >= len(ops) || run-end > 2*context {
				break
			}
			end = run
		}
		hunkEnd := end + context
		if hunkEnd > len(ops) {
			hunkEnd = len(ops)
		}
		oldStart, newStart := ops[hunkStart].oldLine, ops[hunkStart].newLine
		oldCount, newCount := 0, 0
		for index := hunkStart; index < hunkEnd; index++ {
			if ops[index].kind != '+' {
				oldCount++
			}
			if ops[index].kind != '-' {
				newCount++
			}
		}
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", oldStart+1, oldCount, newStart+1, newCount)
		for index := hunkStart; index < hunkEnd; index++ {
			out.WriteByte(ops[index].kind)
			out.WriteString(redactSiteFileDiffLine(ops[index].text))
			out.WriteByte('\n')
			if out.Len() > maxSiteFileDiffBytes {
				return cutDiff(out.String()), true
			}
		}
		start = hunkEnd
	}
	return out.String(), truncated
}

func cutDiff(text string) string {
	if len(text) <= maxSiteFileDiffBytes {
		return text
	}
	cut := strings.LastIndexByte(text[:maxSiteFileDiffBytes], '\n')
	if cut < 0 {
		return ""
	}
	return text[:cut+1]
}

func splitDiffLines(text string) []string {
	if text == "" {
		return nil
	}
	text = strings.TrimSuffix(text, "\n")
	return strings.Split(text, "\n")
}

type diffLineOp struct {
	kind    byte
	text    string
	oldLine int
	newLine int
}

// diffLineOps is a longest-common-subsequence line diff. Both sides are
// bounded by maxSiteFileDiffLines, so the table stays small.
func diffLineOps(oldLines, newLines []string) []diffLineOp {
	n, m := len(oldLines), len(newLines)
	// Trim the common prefix and suffix first.
	prefix := 0
	for prefix < n && prefix < m && oldLines[prefix] == newLines[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < n-prefix && suffix < m-prefix && oldLines[n-1-suffix] == newLines[m-1-suffix] {
		suffix++
	}
	a := oldLines[prefix : n-suffix]
	b := newLines[prefix : m-suffix]
	table := make([][]int32, len(a)+1)
	for i := range table {
		table[i] = make([]int32, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				table[i][j] = table[i+1][j+1] + 1
			} else if table[i+1][j] >= table[i][j+1] {
				table[i][j] = table[i+1][j]
			} else {
				table[i][j] = table[i][j+1]
			}
		}
	}
	ops := make([]diffLineOp, 0, n+m)
	for index := 0; index < prefix; index++ {
		ops = append(ops, diffLineOp{kind: ' ', text: oldLines[index], oldLine: index, newLine: index})
	}
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			ops = append(ops, diffLineOp{kind: ' ', text: a[i], oldLine: prefix + i, newLine: prefix + j})
			i++
			j++
		case table[i+1][j] >= table[i][j+1]:
			ops = append(ops, diffLineOp{kind: '-', text: a[i], oldLine: prefix + i, newLine: prefix + j})
			i++
		default:
			ops = append(ops, diffLineOp{kind: '+', text: b[j], oldLine: prefix + i, newLine: prefix + j})
			j++
		}
	}
	for ; i < len(a); i++ {
		ops = append(ops, diffLineOp{kind: '-', text: a[i], oldLine: prefix + i, newLine: prefix + j})
	}
	for ; j < len(b); j++ {
		ops = append(ops, diffLineOp{kind: '+', text: b[j], oldLine: prefix + i, newLine: prefix + j})
	}
	for index := 0; index < suffix; index++ {
		ops = append(ops, diffLineOp{
			kind: ' ', text: oldLines[n-suffix+index],
			oldLine: n - suffix + index, newLine: m - suffix + index,
		})
	}
	return ops
}

var siteFileSecretLine = regexp.MustCompile(`(?i)(authorization|password|passwd|secret|token|api[_-]?key|private[_-]?key|cookie)`)

// redactSiteFileDiffLine keeps the directive and hides its value when the
// line names something that is usually a credential. Paths (for example
// auth_basic_user_file) are not credentials and stay readable.
func redactSiteFileDiffLine(line string) string {
	if !siteFileSecretLine.MatchString(line) {
		return line
	}
	trimmed := strings.TrimLeft(line, " \t")
	indent := line[:len(line)-len(trimmed)]
	if strings.HasPrefix(trimmed, "#") {
		return indent + "# [hidden by CelikPanel: this line may hold a credential]"
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return line
	}
	if fields[0] == "auth_basic_user_file" || fields[0] == "ssl_certificate_key" || fields[0] == "ssl_password_file" {
		return line
	}
	keep := fields[0]
	if (fields[0] == "proxy_set_header" || fields[0] == "add_header" || fields[0] == "fastcgi_param" ||
		fields[0] == "set" || fields[0] == "more_set_headers") && len(fields) > 1 {
		keep += " " + fields[1]
	}
	return indent + keep + " [hidden by CelikPanel: this value may be a credential];"
}
