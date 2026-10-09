-- NBO-01: preserve the actor opening a staff-assisted application.
-- This is independent of the applicant and remains immutable through all
-- normal application mutations. Existing self-service applications are unchanged.
ALTER TABLE admission.client_application
  ADD COLUMN IF NOT EXISTS opened_by_staff_principal_id uuid REFERENCES identity.principal(principal_id);

ALTER TABLE admission.client_application
  ADD CONSTRAINT staff_created_application_maker
    CHECK (
      opened_by_staff_principal_id IS NULL OR
      (application_channel IN ('INTERNAL_GROUP','ASSISTED_ENTERPRISE')
        AND opened_by_staff_principal_id <> applicant_principal_id)
    );

CREATE INDEX IF NOT EXISTS client_application_staff_maker_idx
  ON admission.client_application(opened_by_staff_principal_id)
  WHERE opened_by_staff_principal_id IS NOT NULL;
