DROP TABLE IF EXISTS awm_memory_candidates;
DROP TABLE IF EXISTS awm_memories;

ALTER TABLE awm_receipts DROP COLUMN IF EXISTS memory_ids;
