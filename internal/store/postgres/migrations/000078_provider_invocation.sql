-- A provider's logical invocation reference (Shared capability/v1
-- registration.schema.json provider.invocation): how callers invoke it,
-- the source of a RESOLVED CapabilityResolution's invocation descriptor.
-- Never a deployment hostname or credential. Written only by engine
-- registration; both are set or neither is.
ALTER TABLE capability.capability_provider
    ADD COLUMN service_reference text
        CHECK (service_reference ~ '^service://[a-z][a-z0-9-]*(/[a-z0-9-]+)*$' AND length(service_reference) <= 255),
    ADD COLUMN invocation_protocol text CHECK (invocation_protocol IN ('http', 'grpc')),
    ADD CONSTRAINT capability_provider_invocation_check CHECK ((service_reference IS NULL) = (invocation_protocol IS NULL));
