package domain

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAgentMemoryLessonsAreStrictAndBounded(t *testing.T) {
	lesson, err := ParseAgentMemoryLesson([]byte(`{"to":"memory","lesson":"  Run  go vet\n before pushing.\u0007 "}`))
	require.NoError(t, err)
	require.Equal(t, "Run go vet before pushing.", lesson, "a lesson is one line without control characters")
	require.True(t, AgentOutboxIsMemory([]byte(`{"to":"Memory","lesson":"x"}`)))
	require.True(t, AgentOutboxIsMemory([]byte(`{"to":"memory","body":"wrong field"}`)), "a malformed memory file is still routed to memory")
	require.False(t, AgentOutboxIsMemory([]byte(`{"to":"ENG-1","body":"x"}`)))
	require.False(t, AgentOutboxIsMemory([]byte(`not json`)))

	for name, data := range map[string]string{
		"body instead of lesson": `{"to":"memory","body":"x"}`,
		"extra field":            `{"to":"memory","lesson":"x","channel":"internal"}`,
		"empty":                  `{"to":"memory","lesson":" \n "}`,
		"not a string":           `{"to":"memory","lesson":["x"]}`,
		"two objects":            `{"to":"memory","lesson":"x"}{"to":"memory","lesson":"y"}`,
		"another recipient":      `{"to":"ENG-1","lesson":"x"}`,
		"truncated":              `{"to":"memory","lesson":"x"`,
		"too long":               `{"to":"memory","lesson":"` + strings.Repeat("a", LessonMaxRunes+1) + `"}`,
		"oversized file":         `{"to":"memory","lesson":"` + strings.Repeat("a", AgentOutboxFileMaxBytes) + `"}`,
	} {
		_, err := ParseAgentMemoryLesson([]byte(data))
		require.Error(t, err, name)
	}
	_, err = ParseAgentMemoryLesson([]byte(`{"to":"memory","lesson":"` + strings.Repeat("è", LessonMaxRunes) + `"}`))
	require.NoError(t, err, "the limit counts characters, not bytes")
	_, err = ParseAgentMemoryLesson([]byte("{\"to\":\"memory\",\"lesson\":\"\xff\"}"))
	require.Error(t, err, "invalid UTF-8")
}

// What an approver reads must be what an agent would read: characters that
// do not show (zero-width, bidirectional overrides) never enter a lesson.
func TestLessonsCarryNoInvisibleCharacters(t *testing.T) {
	hidden := "Run\u200b the\u200f checks\u202e;tnemmoc\u202c \u2066now\u2069\ufeff\u200d."
	lesson, err := CleanLesson(hidden)
	require.NoError(t, err)
	require.Equal(t, "Run the checks;tnemmoc now.", lesson)
	require.Equal(t, "Run the checks;tnemmoc now.", LessonExcerpt(hidden))
	lesson, err = ParseAgentMemoryLesson([]byte(`{"to":"memory","lesson":"Use\u202emain\u202c only"}`))
	require.NoError(t, err)
	require.Equal(t, "Usemain only", lesson)
	_, err = CleanLesson("\u200b\u2067\ufeff")
	require.ErrorIs(t, err, ErrLessonEmpty, "a lesson of invisible characters is empty")
	require.Equal(t, "", LessonExcerpt("\u200e \u202a"))
}

func TestLessonExcerptsFitOneLesson(t *testing.T) {
	require.Equal(t, "", LessonExcerpt(" \n\t "))
	require.Equal(t, "Tests failed: 3 of 9", LessonExcerpt("Tests failed:\n\n 3 of 9"))
	long := LessonExcerpt(strings.Repeat("word ", 200))
	require.LessOrEqual(t, len([]rune(long)), LessonMaxRunes)
	require.True(t, strings.HasSuffix(long, "…"))
	_, err := CleanLesson(long)
	require.NoError(t, err, "an excerpt is a valid lesson")
}

func TestLessonDedupeKeysIgnoreCaseAndSpacing(t *testing.T) {
	require.Equal(t, LessonDedupeKey("task", "Unit  Tests"), LessonDedupeKey("task", " unit tests\n"))
	require.NotEqual(t, LessonDedupeKey("task", "unit tests"), LessonDedupeKey("review", "unit tests"))
	require.Len(t, LessonDedupeKey("task", "x"), 64)
}

func TestFormatLessonsKeepsOrderAndStaysWithinThePromptBudget(t *testing.T) {
	require.Equal(t, "", FormatLessons(nil))
	require.Equal(t, "", FormatLessons([]string{" ", ""}))
	block := FormatLessons([]string{"Run `make check` before pushing.", "Renumber migrations\nafter merging main."})
	require.Equal(t, lessonsHeading+"\n- Run `make check` before pushing.\n- Renumber migrations after merging main.", block)

	many := make([]string, 0, LessonsApprovedPerRepository)
	for i := range LessonsApprovedPerRepository {
		many = append(many, string(rune('a'+i%26))+strings.Repeat("x", LessonMaxRunes-1))
	}
	block = FormatLessons(many)
	require.LessOrEqual(t, len(block), LessonsPromptMaxBytes)
	lines := strings.Split(block, "\n")[1:]
	require.NotEmpty(t, lines)
	require.Less(t, len(lines), len(many), "lessons beyond the budget are left out")
	for i, line := range lines {
		require.Equal(t, "- "+many[i], line, "whole lessons, in order")
	}
}
