package domain

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A page is a line range of the file, bounded by lines and by the bytes its
// JSON answer takes; truncated says the file goes on after the page.
func TestTaskFilePageReadsALineRangeWithinItsBounds(t *testing.T) {
	data := []byte("one\ntwo\nthree\nfour\n")
	content, last, total, truncated := TaskFilePage(data, 1, 0, TaskFileBytesMax)
	require.Equal(t, "one\ntwo\nthree\nfour", content)
	require.Equal(t, 4, last)
	require.Equal(t, 4, total)
	require.False(t, truncated)

	content, last, total, truncated = TaskFilePage(data, 2, 3, TaskFileBytesMax)
	require.Equal(t, "two\nthree", content)
	require.Equal(t, 3, last)
	require.Equal(t, 4, total)
	require.True(t, truncated, "line four is still to read")

	// A last line without a newline is a line; past the end is empty.
	content, last, total, truncated = TaskFilePage([]byte("a\nb"), 2, 0, TaskFileBytesMax)
	require.Equal(t, "b", content)
	require.Equal(t, 2, last)
	require.Equal(t, 2, total)
	require.False(t, truncated)
	content, last, total, truncated = TaskFilePage([]byte("a\nb"), 5, 0, TaskFileBytesMax)
	require.Empty(t, content)
	require.Zero(t, last)
	require.Equal(t, 2, total)
	require.False(t, truncated)
	content, last, total, truncated = TaskFilePage(nil, 1, 0, TaskFileBytesMax)
	require.Empty(t, content)
	require.Zero(t, last+total)
	require.False(t, truncated)
}

func TestTaskFilePageStopsAtTheLineAndByteCaps(t *testing.T) {
	var many strings.Builder
	for range TaskFileLinesMax + 10 {
		many.WriteString("x\n")
	}
	content, last, total, truncated := TaskFilePage([]byte(many.String()), 1, 0, TaskFileBytesMax)
	require.Equal(t, TaskFileLinesMax, last)
	require.Equal(t, TaskFileLinesMax+10, total)
	require.Equal(t, TaskFileLinesMax, strings.Count(content, "\n")+1)
	require.True(t, truncated)
	// Asking for more lines than a page holds still returns one page.
	_, last, _, _ = TaskFilePage([]byte(many.String()), 3, TaskFileLinesMax+9, TaskFileBytesMax)
	require.Equal(t, TaskFileLinesMax+2, last)

	// Bytes are counted as JSON encodes them: quotes and markup escape.
	line := strings.Repeat(`"<`, 10) // 20 bytes, 80 once encoded
	data := []byte(strings.Repeat(line+"\n", 10))
	content, last, total, truncated = TaskFilePage(data, 1, 0, 200)
	require.Equal(t, 2, last, "two lines and their separator take 162 of 200 bytes")
	require.Equal(t, 10, total)
	require.True(t, truncated)
	encoded, err := json.Marshal(content)
	require.NoError(t, err)
	require.LessOrEqual(t, len(encoded)-2, 200)

	// A first line longer than the page is cut on a character boundary.
	content, last, _, truncated = TaskFilePage([]byte(strings.Repeat("é", 100)+"\nnext\n"), 1, 0, 51)
	require.Equal(t, strings.Repeat("é", 25), content)
	require.Equal(t, 1, last)
	require.True(t, truncated)
}

func TestTaskFileBinaryAndPathRules(t *testing.T) {
	require.True(t, IsBinaryContent([]byte("text\x00more")))
	require.False(t, IsBinaryContent([]byte("plain text\n")))
	require.False(t, IsBinaryContent(append([]byte(strings.Repeat("a", 8000)), 0)), "only the first 8,000 bytes are sniffed, as the repository scans do")

	for _, valid := range []string{"README.md", "src/app.go", ".github/workflows/ci.yml", "a b/ç.txt"} {
		require.True(t, ValidTaskFilePath(valid), valid)
	}
	for _, invalid := range []string{"", "/etc/passwd", "../outside", "src/../../x", "./src/app.go", "src//app.go", "src/", ".", ".git/config", "a/.git/HEAD", `src\app.go`, "nul\x00", "line\nbreak", "\xff"} {
		require.False(t, ValidTaskFilePath(invalid), invalid)
	}
	require.True(t, ValidCommitID(strings.Repeat("a", 40)))
	require.True(t, ValidCommitID(strings.Repeat("0", 64)))
	for _, invalid := range []string{"", "HEAD", "main", strings.Repeat("A", 40), strings.Repeat("a", 39), strings.Repeat("a", 41), "abc1234"} {
		require.False(t, ValidCommitID(invalid), invalid)
	}
}
