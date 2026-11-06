-- Workspaces that chose SearXNG fall back to the default provider, Brave; a
-- stored Brave or Tavily key is kept. Their SearXNG choice is not restored by
-- migrating up again.
UPDATE assistant_research_settings SET search_provider = 'brave', updated_at = now() WHERE search_provider = 'searxng';
ALTER TABLE assistant_research_settings DROP CONSTRAINT IF EXISTS assistant_research_settings_search_provider_check;
ALTER TABLE assistant_research_settings ADD CONSTRAINT assistant_research_settings_search_provider_check
    CHECK (search_provider IN ('brave', 'tavily'));
