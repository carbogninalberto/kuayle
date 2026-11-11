package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Workspace memory (KUA-134): short lessons per repository. Kuayle and agents
// only propose them; a workspace owner or admin approves, edits or drops each
// one, and only approved lessons, as that person left them, reach a prompt.
const (
	LessonProposed = "proposed"
	LessonApproved = "approved"
	LessonRetired  = "retired"
	LessonDropped  = "dropped"

	// Where a lesson came from.
	LessonEventTask       = "task"       // an agent proposed it through its outbox
	LessonEventCorrection = "correction" // a person messaged or commented on a task
	LessonEventReview     = "review"     // a review requested changes
	LessonEventCI         = "ci"         // a CI check failed
	LessonEventManual     = "manual"     // a member wrote it in Settings

	// Who wrote the proposed text.
	LessonProposerAgent  = "agent"  // an agent's own words: never an instruction
	LessonProposerKuayle = "kuayle" // derived by Kuayle from CI or a GitHub review
	LessonProposerPerson = "person" // a workspace member's own words

	// LessonMaxRunes bounds one lesson: a sentence or two.
	LessonMaxRunes = 400
	// LessonsApprovedPerRepository bounds the approved lessons of a repository.
	LessonsApprovedPerRepository = 30
	// LessonsPendingPerRepository bounds the proposals waiting on a repository;
	// further proposals are not recorded until a person decides some.
	LessonsPendingPerRepository = 50
	// LessonProposalsPerTaskEvent bounds what one task may propose from one
	// kind of event, whatever became of the proposals.
	LessonProposalsPerTaskEvent = 3
	// LessonsPromptMaxBytes bounds the block of lessons placed in a prompt.
	LessonsPromptMaxBytes = 4000
)

// AgentOutboxMemory addresses an outbox file to the workspace memory:
// {"to":"memory","lesson":"..."} proposes a lesson for the task's repository.
const AgentOutboxMemory = "MEMORY"

var (
	ErrLessonEmpty   = errors.New("a lesson needs some text")
	ErrLessonTooLong = errors.New("a lesson is at most 400 characters")
)

// Lesson is one lesson of a repository. Text is what agents see once the
// lesson is approved; ProposedText is what was proposed, kept as written.
// TeamID is set for a lesson that came from a private team's task: only
// people who can see that team see it, and only that team's tasks receive it.
type Lesson struct {
	ID           uuid.UUID  `json:"id" db:"id"`
	WorkspaceID  uuid.UUID  `json:"-" db:"workspace_id"`
	Repository   string     `json:"repository" db:"repository_full_name"`
	TeamID       *uuid.UUID `json:"team_id" db:"team_id"`
	State        string     `json:"state" db:"state"`
	Text         string     `json:"text" db:"text"`
	ProposedText string     `json:"proposed_text" db:"proposed_text"`
	SourceEvent  string     `json:"source_event" db:"source_event"`
	Proposer     string     `json:"proposer" db:"proposer"`
	SourceTaskID *uuid.UUID `json:"source_task_id" db:"source_task_id"`
	ProposedBy   *uuid.UUID `json:"proposed_by" db:"proposed_by"`
	DecidedBy    *uuid.UUID `json:"decided_by" db:"decided_by"`
	DecidedAt    *time.Time `json:"decided_at" db:"decided_at"`
	Position     int        `json:"position" db:"position"`
	CreatedAt    time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at" db:"updated_at"`
}

// LessonEvent is one audited change to a lesson.
type LessonEvent struct {
	ID        int64           `json:"id" db:"id"`
	LessonID  uuid.UUID       `json:"lesson_id" db:"lesson_id"`
	ActorID   *uuid.UUID      `json:"actor_id" db:"actor_id"`
	Action    string          `json:"action" db:"action"`
	Detail    json.RawMessage `json:"detail" db:"detail"`
	CreatedAt time.Time       `json:"created_at" db:"created_at"`
}

// CleanLesson makes text one line without control characters and refuses an
// empty or overlong lesson.
func CleanLesson(text string) (string, error) {
	if !utf8.ValidString(text) {
		return "", ErrLessonEmpty
	}
	text = strings.Join(strings.Fields(cleanAgentMessage(text)), " ")
	if text == "" {
		return "", ErrLessonEmpty
	}
	if utf8.RuneCountInString(text) > LessonMaxRunes {
		return "", ErrLessonTooLong
	}
	return text, nil
}

// LessonExcerpt turns longer text Kuayle derives a proposal from (review
// findings, a CI summary, a person's message) into one lesson-sized line, cut
// with an ellipsis. It returns "" for text without content.
func LessonExcerpt(text string) string {
	text = strings.Join(strings.Fields(cleanAgentMessage(strings.ToValidUTF8(text, ""))), " ")
	if runes := []rune(text); len(runes) > LessonMaxRunes {
		text = strings.TrimSpace(string(runes[:LessonMaxRunes-1])) + "…"
	}
	return text
}

// LessonDedupeKey identifies a proposal within a repository: the same subject
// is proposed once, whatever a person later decides about it.
func LessonDedupeKey(event, subject string) string {
	digest := sha256.Sum256([]byte(event + "\x00" + strings.ToLower(strings.Join(strings.Fields(subject), " "))))
	return hex.EncodeToString(digest[:])
}

// AgentOutboxIsMemory reports whether an outbox file is addressed to the
// workspace memory, valid or not.
func AgentOutboxIsMemory(data []byte) bool { return AgentMessageRef(data) == AgentOutboxMemory }

// ParseAgentMemoryLesson reads {"to":"memory","lesson":"..."} strictly: one
// JSON object with only those fields and a lesson of at most LessonMaxRunes
// characters. The text stays untrusted agent text.
func ParseAgentMemoryLesson(data []byte) (string, error) {
	if len(data) > AgentOutboxFileMaxBytes {
		return "", errors.New("message file is too large")
	}
	if !utf8.Valid(data) {
		return "", errors.New("message is not valid UTF-8")
	}
	var raw struct {
		To     *string `json:"to"`
		Lesson *string `json:"lesson"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil || raw.To == nil || raw.Lesson == nil {
		return "", errors.New("a lesson is {\"to\":\"memory\",\"lesson\":\"...\"}")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return "", errors.New("invalid JSON message")
	}
	if !strings.EqualFold(strings.TrimSpace(*raw.To), AgentOutboxMemory) {
		return "", errors.New("invalid recipient")
	}
	return CleanLesson(*raw.Lesson)
}

// lessonsHeading introduces approved lessons in a prompt.
const lessonsHeading = "Lessons for this repository (approved by the workspace's admins):"

// FormatLessons renders approved lessons, in order, as a plain block for a
// prompt of at most LessonsPromptMaxBytes bytes. A lesson that does not fit
// is left out whole, with every lesson after it, so order decides what an
// agent sees. It returns "" when there is nothing to say.
func FormatLessons(lessons []string) string {
	var b strings.Builder
	for _, lesson := range lessons {
		lesson = strings.Join(strings.Fields(lesson), " ")
		if lesson == "" {
			continue
		}
		line := "- " + lesson + "\n"
		if b.Len() == 0 {
			line = lessonsHeading + "\n" + line
		}
		if b.Len()+len(line) > LessonsPromptMaxBytes {
			break
		}
		b.WriteString(line)
	}
	return strings.TrimRight(b.String(), "\n")
}
