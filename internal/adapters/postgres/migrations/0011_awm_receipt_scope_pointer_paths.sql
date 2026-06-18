ALTER TABLE awm_receipts
	ADD COLUMN IF NOT EXISTS pointer_paths TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[];

UPDATE awm_receipts AS r
SET pointer_paths = scope.pointer_paths
FROM (
	SELECT
		receipt_id,
		COALESCE(
			ARRAY_AGG(DISTINCT path ORDER BY path) FILTER (WHERE path IS NOT NULL),
			ARRAY[]::TEXT[]
		) AS pointer_paths
	FROM (
		SELECT
			r2.receipt_id,
			p.path
		FROM awm_receipts r2
		LEFT JOIN LATERAL unnest(r2.pointer_keys) AS pk(pointer_key) ON TRUE
		LEFT JOIN awm_pointers p
			ON p.project_id = r2.project_id
			AND p.pointer_key = pk.pointer_key
	) AS receipt_paths
	GROUP BY receipt_id
) AS scope
WHERE r.receipt_id = scope.receipt_id;
