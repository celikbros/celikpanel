-- Persist the outcome of every scheduled-backup attempt so operators can
-- distinguish a healthy schedule from one that has silently stopped working.
ALTER TABLE backup_schedules ADD COLUMN last_attempt TEXT;
ALTER TABLE backup_schedules ADD COLUMN last_status TEXT;
ALTER TABLE backup_schedules ADD COLUMN last_error TEXT;
