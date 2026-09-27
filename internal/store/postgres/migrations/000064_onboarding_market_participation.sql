-- Onboarding requests declare market participation (Shared admission/v1,
-- ADR-BCP-011 section 6): for each admitted market, what the tenant will do
-- there. Requests made before this carry none; provisioning plans their
-- markets as blocked rather than inventing activities.
ALTER TABLE admission.tenant_onboarding_request
    ADD COLUMN market_participation jsonb NOT NULL DEFAULT '[]'
        CHECK (jsonb_typeof(market_participation) = 'array');
