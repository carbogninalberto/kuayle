-- Irreversible data effect: issue identifiers already rewritten to a new team
-- key stay as they are; only the old-key redirects are lost.
DROP TABLE IF EXISTS team_key_aliases;
