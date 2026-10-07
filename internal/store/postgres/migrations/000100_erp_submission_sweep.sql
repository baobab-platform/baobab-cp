-- Recovery sweep of ERP submissions (FB-04d).
--
-- ERP progress normally arrives as signed provisioning.changed events (FB-04c). The sweep is the insurance for an event that was
-- lost or is overdue: it reads the operation from ERP and records it through the same forward-only path. These two columns bound
-- it. Both are bookkeeping about the sweep, never about the submission, so the fixed-columns trigger does not cover them.
--   sweep_attempts  how many recovery reads were made since the operation last advanced; it drives the back-off and is reset
--                   when a newer ERP state is recorded.
--   last_swept_at   when the last recovery read was claimed; the claim is the lease, so concurrent instances do not read twice.
ALTER TABLE provisioning.erp_submission
    ADD COLUMN sweep_attempts integer NOT NULL DEFAULT 0 CHECK (sweep_attempts >= 0),
    ADD COLUMN last_swept_at  timestamptz;

CREATE INDEX erp_submission_open_idx ON provisioning.erp_submission (updated_at)
    WHERE last_state NOT IN ('active', 'failed', 'cancelled');
