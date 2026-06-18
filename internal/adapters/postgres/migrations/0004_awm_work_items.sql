CREATE TABLE IF NOT EXISTS awm_work_items (
	work_item_id BIGSERIAL PRIMARY KEY,
	project_id TEXT NOT NULL,
	receipt_id TEXT NOT NULL,
	item_key TEXT NOT NULL,
	status TEXT NOT NULL CHECK (status IN ('pending', 'in_progress', 'blocked', 'complete')),
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	UNIQUE (project_id, receipt_id, item_key),
	FOREIGN KEY (receipt_id)
		REFERENCES awm_receipts (receipt_id)
		ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_awm_work_items_project_receipt
	ON awm_work_items (project_id, receipt_id, item_key);

CREATE INDEX IF NOT EXISTS idx_awm_work_items_project_receipt_status
	ON awm_work_items (project_id, receipt_id, status, item_key);

CREATE INDEX IF NOT EXISTS idx_awm_work_items_project_receipt_updated
	ON awm_work_items (project_id, receipt_id, updated_at DESC);
