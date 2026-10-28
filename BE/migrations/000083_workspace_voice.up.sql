-- The speech-to-text model a workspace uses for the assistant microphone and
-- Telegram voice notes. No row means voice is off. The model files themselves
-- live in the voice service's volume; only the owner's choice is stored here.
-- No audio or transcript is stored by this feature.
CREATE TABLE workspace_voice_settings (
    workspace_id UUID PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    model TEXT NOT NULL DEFAULT 'off' CHECK (model IN (
        'off',
        'faster-whisper-base',
        'faster-whisper-small',
        'faster-whisper-medium',
        'faster-whisper-large-v3-turbo',
        'vosk-small-en-us-0.15',
        'vosk-small-it-0.22'
    )),
    updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
