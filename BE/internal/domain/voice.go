package domain

// VoiceModelOff is the stored choice when a workspace has no voice model.
const VoiceModelOff = "off"

// Voice transcription limits, enforced by the API, the Telegram intake and
// the voice service itself.
const (
	VoiceMaxSeconds = 300
	VoiceMaxBytes   = 20 << 20
)

// VoiceModel is one speech-to-text model the owner can choose. The voice
// service holds the pinned download sources and checksums; the backend only
// knows the identifiers (also enforced by the database check constraint).
type VoiceModel struct {
	ID     string `json:"id"`
	Engine string `json:"engine"`
	// Language is empty for multilingual models that detect the language.
	Language string `json:"language"`
	// SizeMB is the approximate download size.
	SizeMB int `json:"size_mb"`
}

var VoiceModels = []VoiceModel{
	{ID: "faster-whisper-base", Engine: "faster-whisper", SizeMB: 148},
	{ID: "faster-whisper-small", Engine: "faster-whisper", SizeMB: 486},
	{ID: "faster-whisper-medium", Engine: "faster-whisper", SizeMB: 1530},
	{ID: "faster-whisper-large-v3-turbo", Engine: "faster-whisper", SizeMB: 1622},
	{ID: "vosk-small-en-us-0.15", Engine: "vosk", Language: "en", SizeMB: 41},
	{ID: "vosk-small-it-0.22", Engine: "vosk", Language: "it", SizeMB: 50},
}

// ValidVoiceModel reports whether id is "off" or a known model.
func ValidVoiceModel(id string) bool {
	if id == VoiceModelOff {
		return true
	}
	for _, m := range VoiceModels {
		if m.ID == id {
			return true
		}
	}
	return false
}
