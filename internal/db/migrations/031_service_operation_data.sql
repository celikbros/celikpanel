-- Composite host mutations keep their private, restart-safe execution plan in
-- the operation row. The column is deliberately not exposed by the public
-- operation API; callers only see sanitized phase/result/error fields.
ALTER TABLE service_operations ADD COLUMN operation_data TEXT;
