-- Keep one durable logical identity from the moment a scheduled backup is
-- claimed until the panel has observed its published result. RPC timeouts
-- therefore retry the same agent operation instead of creating another copy.
ALTER TABLE backup_schedules ADD COLUMN active_job_key TEXT;
