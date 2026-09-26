-- ADR-SHARED-013 (Consequences): the Control Plane moves its canonical-to-
-- canonical mappings into mapping.mapping, the store the administrative API
-- governs, and the runtime resolver reads that store from now on.
--
-- A legacy row is migrated with the semantics the resolver already applied to
-- it, never with new ones: its repository filled in SOURCE_TO_TARGET,
-- ONE_TO_ONE, CONFIRMED, resolution priority 0 and no scope (its scope match
-- never matched a request context), for the tenant of the source entity. The
-- authority is the Control Plane, which recorded the row. metadata.source
-- marks every migrated row, and mapping.legacy_canonical_mapping_report lists
-- every legacy row with what happened to it. A row this cannot be established
-- for is reported, not migrated:
--   TENANT_UNKNOWN            the source entity has no tenant;
--   CROSS_TENANT              the target belongs to another tenant (Canonical
--                             Mapping Model section 47);
--   MAPPING_TYPE_UNREGISTERED the type is not a registered mappingType;
--   OVERLAPS_ACTIVE_MAPPING   an identical ACTIVE mapping already in
--                             mapping.mapping overlaps its validity period.
-- Additive: mapping.canonical_mapping keeps its rows and is frozen.

CREATE FUNCTION mapping.registered_mapping_type(candidate text) RETURNS boolean
LANGUAGE sql IMMUTABLE AS $$
    SELECT candidate IN ('IDENTITY', 'REPRESENTATION', 'ORGANISATIONAL', 'CONTENT', 'COMMERCE', 'ERP',
        'CATALOGUE', 'PRICING', 'TAX', 'WAREHOUSE', 'FULFILMENT', 'PAYMENT', 'DOMAIN', 'LOCALE', 'CURRENCY', 'CHANNEL',
        'CAPABILITY', 'INTEGRATION', 'MIGRATION', 'ALIAS', 'SUCCESSOR')
$$;

-- Copies every legacy row not yet migrated and returns how many it copied, so
-- running it again changes nothing. A row that would overlap an ACTIVE mapping
-- already in mapping.mapping is skipped and reported, never forced.
CREATE FUNCTION mapping.migrate_legacy_canonical_mappings() RETURNS bigint
LANGUAGE sql AS $$
    WITH copied AS (
        INSERT INTO mapping.mapping (
            mapping_id, tenant_id, mapping_type, canonical_entity_id, target_canonical_entity_id,
            direction, cardinality, authority, confidence, resolution_priority, status,
            effective_from, effective_to, metadata, created_at, created_by, updated_at)
        SELECT
            'map_' || replace(cm.canonical_mapping_id::text, '-', ''),
            source.tenant_id,
            upper(cm.mapping_type),
            cm.source_entity_id,
            cm.target_entity_id,
            'SOURCE_TO_TARGET', 'ONE_TO_ONE', 'control-plane', 'CONFIRMED', 0,
            upper(cm.status),
            cm.effective_from, cm.effective_to,
            jsonb_build_object(
                'source', 'legacy-canonical-mapping',
                'legacy_canonical_mapping_id', cm.canonical_mapping_id::text,
                'legacy_mapping_type', cm.mapping_type),
            cm.created_at, 'migration:000060', now()
        FROM mapping.canonical_mapping cm
        JOIN registry.canonical_entity source ON source.canonical_entity_id = cm.source_entity_id
        JOIN registry.canonical_entity target ON target.canonical_entity_id = cm.target_entity_id
        WHERE source.tenant_id IS NOT NULL
          AND target.tenant_id IS NOT DISTINCT FROM source.tenant_id
          AND mapping.registered_mapping_type(upper(cm.mapping_type))
        ORDER BY cm.created_at, cm.canonical_mapping_id
        ON CONFLICT DO NOTHING
        RETURNING 1)
    SELECT count(*) FROM copied
$$;

SELECT mapping.migrate_legacy_canonical_mappings();

CREATE VIEW mapping.legacy_canonical_mapping_report AS
SELECT
    cm.canonical_mapping_id AS legacy_row_id,
    cm.source_entity_id,
    cm.target_entity_id,
    cm.mapping_type,
    cm.status,
    cm.created_at,
    m.mapping_id,
    CASE
        WHEN m.mapping_id IS NOT NULL THEN 'MIGRATED'
        WHEN source.tenant_id IS NULL THEN 'TENANT_UNKNOWN'
        WHEN target.tenant_id IS DISTINCT FROM source.tenant_id THEN 'CROSS_TENANT'
        WHEN NOT mapping.registered_mapping_type(upper(cm.mapping_type)) THEN 'MAPPING_TYPE_UNREGISTERED'
        ELSE 'OVERLAPS_ACTIVE_MAPPING'
    END AS disposition
FROM mapping.canonical_mapping cm
JOIN registry.canonical_entity source ON source.canonical_entity_id = cm.source_entity_id
JOIN registry.canonical_entity target ON target.canonical_entity_id = cm.target_entity_id
LEFT JOIN mapping.mapping m ON m.mapping_id = 'map_' || replace(cm.canonical_mapping_id::text, '-', '');

CREATE OR REPLACE FUNCTION mapping.legacy_canonical_mapping_frozen() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'mapping.canonical_mapping is superseded by mapping.mapping (ADR-SHARED-013)'
        USING ERRCODE = 'read_only_sql_transaction';
END;
$$;
CREATE TRIGGER legacy_canonical_mapping_frozen
    BEFORE INSERT OR UPDATE ON mapping.canonical_mapping
    FOR EACH ROW EXECUTE FUNCTION mapping.legacy_canonical_mapping_frozen();
