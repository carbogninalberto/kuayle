CREATE TABLE user_preferences (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    font_size VARCHAR(20) NOT NULL DEFAULT 'default',
    pointer_cursors BOOLEAN NOT NULL DEFAULT true,
    theme_mode VARCHAR(20) NOT NULL DEFAULT 'dark',
    light_theme VARCHAR(30) NOT NULL DEFAULT 'light',
    dark_theme VARCHAR(30) NOT NULL DEFAULT 'dark',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
