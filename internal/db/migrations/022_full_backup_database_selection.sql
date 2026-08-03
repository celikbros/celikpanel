-- A "full" scheduled backup is meaningful only when its database selection is
-- durable. Existing schedules deliberately receive an empty selection: the
-- user must explicitly choose databases in the panel before the next full run.
ALTER TABLE backup_schedules
ADD COLUMN database_ids TEXT NOT NULL DEFAULT '[]'
CHECK (
    json_valid(database_ids)
    AND json_type(database_ids) = 'array'
);
