-- Housekeeping can run on a schedule like scans and duplicate checks.
-- Migrations replay on every start, so only swap the check once.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint
                   WHERE conrelid = 'scan_schedule'::regclass
                     AND conname = 'scan_schedule_job_kind_check'
                     AND pg_get_constraintdef(oid) LIKE '%HOUSEKEEPING%') THEN
        ALTER TABLE scan_schedule DROP CONSTRAINT IF EXISTS scan_schedule_job_kind_check;
        ALTER TABLE scan_schedule ADD CONSTRAINT scan_schedule_job_kind_check
            CHECK (job_kind IN ('SCAN', 'DUPLICATE_SCAN', 'HOUSEKEEPING'));
    END IF;
END $$;
