-- +goose NO TRANSACTION
-- +goose Up
-- The artwork revision GC asks "does any catalog surface still reference this
-- path?" before deleting an object. artworkReferenceUnionSQL builds that check
-- as a UNION ALL over every sweep surface, filtering on each surface's pathCol.
--
-- None of those columns is indexed. Only media_items is large enough to matter
-- (625k rows on the deployment this was measured on; every other surface is
-- under 500 rows), and the union scans it three times -- poster_path,
-- backdrop_path, logo_path. A batched check over 10,000 candidate paths ran for
-- more than ten minutes; with these three indexes it returns in ~310ms.
--
-- Partial on IS NOT NULL: a row with no artwork can never match a candidate
-- path, and excluding those keeps the indexes to 89/15/11 MB.
--
-- CONCURRENTLY so this does not take a write lock on a running server, which
-- requires NO TRANSACTION. Note that CREATE INDEX CONCURRENTLY waits for every
-- in-flight transaction to commit, so a long-running GC pass blocks it until
-- that pass finishes.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_media_items_poster_path_gc ON public.media_items (poster_path) WHERE poster_path IS NOT NULL;
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_media_items_backdrop_path_gc ON public.media_items (backdrop_path) WHERE backdrop_path IS NOT NULL;
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_media_items_logo_path_gc ON public.media_items (logo_path) WHERE logo_path IS NOT NULL;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS public.idx_media_items_poster_path_gc;
DROP INDEX CONCURRENTLY IF EXISTS public.idx_media_items_backdrop_path_gc;
DROP INDEX CONCURRENTLY IF EXISTS public.idx_media_items_logo_path_gc;
