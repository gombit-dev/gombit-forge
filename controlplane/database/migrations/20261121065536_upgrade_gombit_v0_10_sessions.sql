-- Modify "refresh_tokens" table
ALTER TABLE "refresh_tokens" ADD COLUMN "session_id" character varying(32) NULL, ADD COLUMN "user_agent" character varying(255) NULL, ADD COLUMN "client_address" character varying(64) NULL, ADD COLUMN "location" character varying(128) NULL;
-- Create index "idx_refresh_tokens_session_id" to table: "refresh_tokens"
CREATE INDEX "idx_refresh_tokens_session_id" ON "refresh_tokens" ("session_id");
