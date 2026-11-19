-- Modify "users" table
ALTER TABLE "users" ADD COLUMN "disabled_at" timestamptz NULL, ADD COLUMN "email_verified_at" timestamptz NULL, ADD COLUMN "verified_email" character varying(255) NOT NULL DEFAULT '';
-- Create "auth_audit_events" table
CREATE TABLE "auth_audit_events" (
  "id" bigserial NOT NULL,
  "occurred_at" timestamptz NOT NULL,
  "action" character varying(64) NOT NULL,
  "actor_user_id" bigint NULL,
  "target_user_id" bigint NULL,
  "target_type" text NULL,
  "target_id" text NULL,
  "request_id" character varying(128) NULL,
  "ip" character varying(64) NULL,
  "user_agent" character varying(255) NULL,
  "details" text NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_auth_audit_events_action" to table: "auth_audit_events"
CREATE INDEX "idx_auth_audit_events_action" ON "auth_audit_events" ("action");
-- Create index "idx_auth_audit_events_actor_user_id" to table: "auth_audit_events"
CREATE INDEX "idx_auth_audit_events_actor_user_id" ON "auth_audit_events" ("actor_user_id");
-- Create index "idx_auth_audit_events_occurred_at" to table: "auth_audit_events"
CREATE INDEX "idx_auth_audit_events_occurred_at" ON "auth_audit_events" ("occurred_at");
-- Create index "idx_auth_audit_events_request_id" to table: "auth_audit_events"
CREATE INDEX "idx_auth_audit_events_request_id" ON "auth_audit_events" ("request_id");
-- Create index "idx_auth_audit_events_target_user_id" to table: "auth_audit_events"
CREATE INDEX "idx_auth_audit_events_target_user_id" ON "auth_audit_events" ("target_user_id");
-- Create "auth_identity_links" table
CREATE TABLE "auth_identity_links" (
  "id" bigserial NOT NULL,
  "user_id" bigint NOT NULL,
  "provider" character varying(32) NOT NULL,
  "subject_key" character varying(64) NOT NULL,
  "subject" character varying(255) NOT NULL,
  "email" character varying(255) NOT NULL DEFAULT '',
  "last_used_at" timestamptz NULL,
  "created_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_auth_identity_links_provider_subject" to table: "auth_identity_links"
CREATE UNIQUE INDEX "idx_auth_identity_links_provider_subject" ON "auth_identity_links" ("provider", "subject_key");
-- Create index "idx_auth_identity_links_user_id" to table: "auth_identity_links"
CREATE INDEX "idx_auth_identity_links_user_id" ON "auth_identity_links" ("user_id");
-- Create "auth_one_time_tokens" table
CREATE TABLE "auth_one_time_tokens" (
  "id" bigserial NOT NULL,
  "user_id" bigint NOT NULL,
  "purpose" character varying(32) NOT NULL,
  "token_hash" character varying(64) NOT NULL,
  "email" character varying(255) NOT NULL,
  "from_email" character varying(255) NOT NULL DEFAULT '',
  "expires_at" timestamptz NOT NULL,
  "sent_at" timestamptz NULL,
  "used_at" timestamptz NULL,
  "created_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_auth_one_time_tokens_expires_at" to table: "auth_one_time_tokens"
CREATE INDEX "idx_auth_one_time_tokens_expires_at" ON "auth_one_time_tokens" ("expires_at");
-- Create index "idx_auth_one_time_tokens_token_hash" to table: "auth_one_time_tokens"
CREATE UNIQUE INDEX "idx_auth_one_time_tokens_token_hash" ON "auth_one_time_tokens" ("token_hash");
-- Create index "idx_auth_one_time_tokens_user_purpose" to table: "auth_one_time_tokens"
CREATE INDEX "idx_auth_one_time_tokens_user_purpose" ON "auth_one_time_tokens" ("user_id", "purpose");
-- Create "auth_recovery_codes" table
CREATE TABLE "auth_recovery_codes" (
  "id" bigserial NOT NULL,
  "user_id" bigint NOT NULL,
  "code_hash" character varying(64) NOT NULL,
  "used_at" timestamptz NULL,
  "created_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_auth_recovery_codes_code_hash" to table: "auth_recovery_codes"
CREATE UNIQUE INDEX "idx_auth_recovery_codes_code_hash" ON "auth_recovery_codes" ("code_hash");
-- Create index "idx_auth_recovery_codes_user_id" to table: "auth_recovery_codes"
CREATE INDEX "idx_auth_recovery_codes_user_id" ON "auth_recovery_codes" ("user_id");
-- Create "auth_totp_factors" table
CREATE TABLE "auth_totp_factors" (
  "id" bigserial NOT NULL,
  "user_id" bigint NOT NULL,
  "secret" character varying(255) NOT NULL,
  "confirmed_at" timestamptz NULL,
  "last_step" bigint NOT NULL DEFAULT 0,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_auth_totp_factors_user_id" to table: "auth_totp_factors"
CREATE UNIQUE INDEX "idx_auth_totp_factors_user_id" ON "auth_totp_factors" ("user_id");
