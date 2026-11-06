-- Workspaces may choose a self-hosted SearXNG instance for web_search. Its
-- address is operator configuration (WEB_SEARCH_SEARXNG_URL), never stored
-- here; the server offers searxng only while that is set.
ALTER TABLE assistant_research_settings DROP CONSTRAINT IF EXISTS assistant_research_settings_search_provider_check;
ALTER TABLE assistant_research_settings ADD CONSTRAINT assistant_research_settings_search_provider_check
    CHECK (search_provider IN ('brave', 'tavily', 'searxng'));
