-- Irreversible for deleted pages: soft-deleted pages are purged (with their
-- revisions and references) because the restored table cannot hide them.

DELETE FROM wiki_page_records WHERE deleted_at IS NOT NULL;

DROP VIEW wiki_pages;
DROP INDEX idx_wiki_page_records_deleted_at;

ALTER TABLE wiki_page_records
    DROP COLUMN deleted_registry_date,
    DROP COLUMN deleted_by,
    DROP COLUMN deleted_at;

ALTER TABLE wiki_page_records RENAME TO wiki_pages;

CREATE OR REPLACE FUNCTION public.wiki_pages_lift_children()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    UPDATE wiki_pages SET parent_id = OLD.parent_id WHERE parent_id = OLD.id;
    RETURN OLD;
END;
$function$
;
