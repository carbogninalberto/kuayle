-- Rolling back forgets each workspace's voice model choice (voice is off).
-- Downloaded models stay in the voice service's volume.
DROP TABLE IF EXISTS workspace_voice_settings;
