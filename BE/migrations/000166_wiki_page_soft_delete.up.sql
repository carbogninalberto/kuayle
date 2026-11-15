-- Wiki page soft delete (KUA-51), the same way as issues in 000160: the table
-- becomes wiki_page_records and "wiki_pages" a simple, automatically
-- updatable view over the pages that are not deleted. Every existing query
-- keeps working and hides deleted pages. Restore and purge name
-- wiki_page_records. New columns must be added to wiki_page_records and the
-- view recreated with them.
--
-- A deleted registry page gives up its registry date (kept in
-- deleted_registry_date), so the registry can start a new page for that day;
-- the unique registry indexes and their ON CONFLICT targets stay unchanged.

ALTER TABLE wiki_pages RENAME TO wiki_page_records;

ALTER TABLE wiki_page_records
    ADD COLUMN deleted_at TIMESTAMPTZ,
    ADD COLUMN deleted_by UUID REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN deleted_registry_date DATE;

CREATE INDEX idx_wiki_page_records_deleted_at ON wiki_page_records(deleted_at) WHERE deleted_at IS NOT NULL;

CREATE VIEW wiki_pages AS
SELECT id, workspace_id, team_id, project_id, title, content, version, created_by,
       updated_by, created_at, updated_at, registry_date, parent_id, position, kind,
       publish_mode, publish_children
FROM wiki_page_records
WHERE deleted_at IS NULL;

-- A purged page lifts its children, deleted ones included, to its parent.
CREATE OR REPLACE FUNCTION public.wiki_pages_lift_children()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    UPDATE wiki_page_records SET parent_id = OLD.parent_id WHERE parent_id = OLD.id;
    RETURN OLD;
END;
$function$;
