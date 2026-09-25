-- Gate 2 (docs/reconciliation/platform-resolution-spine-audit.md): "Complete
-- PostgreSQL status, scope, FK and temporal invariants." This migration is
-- the first slice of that gate, scoped to mapping.mapping_scope specifically.
--
-- 000024 already brought mapping_scope from its original 6 columns up to 16
-- (legal_entity_id, country_code, digital_estate_id, digital_property_id,
-- channel_id, currency_code, locale, environment, engine_id,
-- engine_instance_id), but baobab-platform/shared's contracts/control-plane/v1/
-- canonical-mapping.schema.json #/$defs/mappingScope (and the Baobab
-- Canonical Mapping Model spec, section 10.2, that schema implements)
-- defines ~10 more dimensions this table still had no column for at all:
-- organisation_id, business_unit_id, operating_region_id,
-- geographic_region_id, catalogue_id, customer_segment_id,
-- deployment_region, include_countries, exclude_countries, and updated_at.
-- domain.MappingScope (internal/domain/resolution.go) already carries Go
-- fields for every one of these (Gate 1, #61) with nothing in Postgres to
-- read or write them from -- this migration is what makes that possible.
--
-- Deliberately NOT addressed here (a real, separate gap, not silently
-- worked around): market_id, digital_property_id, engine_id and
-- engine_instance_id are uuid columns FK-referencing this database's own
-- registries (market.market, topology.engine, topology.engine_instance),
-- which is how every other uuid-keyed relationship in this schema works --
-- but the wire schema's own market_id/digital_property_id/engine_id/
-- engine_instance_id are all slug-patterned strings
-- (^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$), not UUIDs. topology.engine has a
-- human-readable `code` column that could stand in for engine_id, but
-- topology.engine_instance has no equivalent slug column at all, so there
-- is no consistent fix available purely on the baobab-cp side. That is an
-- ID-generation-scheme disagreement between this database's registries and
-- baobab-platform/shared's grammar for this concept, the same class of gap Gate 1
-- (#61) already found and flagged for Mapping's own mapping_id/tenant_id
-- (map_.../tn_... vs gen_random_uuid()) -- left for whoever picks up that
-- coordinated decision, not invented unilaterally here.

-- include_countries/exclude_countries deliberately carry no per-element
-- format CHECK: PostgreSQL CHECK expressions cannot contain subqueries (so
-- validating "every array element matches ^[A-Z]{2}$" needs either a stored
-- function or application-side validation), and no other newly-added
-- column here has a format CHECK either -- per-field format is the JSON
-- Schema layer's job (contracts/control-plane/v1/canonical-mapping.schema.json
-- already declares this pattern), consistent with this table's own existing
-- country_code/currency_code CHECKs being the exception (direct-column
-- regexes, not array-element ones) rather than the rule.
ALTER TABLE mapping.mapping_scope
    ADD COLUMN organisation_id text,
    ADD COLUMN business_unit_id text,
    ADD COLUMN operating_region_id text,
    ADD COLUMN geographic_region_id text,
    ADD COLUMN catalogue_id text,
    ADD COLUMN customer_segment_id text,
    ADD COLUMN deployment_region text,
    ADD COLUMN include_countries text[],
    ADD COLUMN exclude_countries text[],
    ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();

-- entity_type (000009) predates this table's alignment with the wire
-- schema and has no equivalent in canonical-mapping.schema.json's
-- mappingScope definition at all. domain.MappingScope's own EntityType
-- field was already removed for the same reason during Gate 1 (#61: "not
-- in the wire schema, and had zero usages anywhere in this repository") --
-- this is that same decision reaching the column it left behind, which the
-- NOT NULL constraint would otherwise force every future insert to fabricate
-- a value for. Not dropped outright: existing rows and any code still
-- reading it (there is none in this repository) are unaffected.
ALTER TABLE mapping.mapping_scope
    ALTER COLUMN entity_type DROP NOT NULL;
