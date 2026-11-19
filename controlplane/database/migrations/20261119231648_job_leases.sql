-- Modify "build_jobs" table
ALTER TABLE "build_jobs" ADD COLUMN "lease_expires_at" timestamptz NULL;
-- Modify "export_jobs" table
ALTER TABLE "export_jobs" ADD COLUMN "lease_expires_at" timestamptz NULL;
