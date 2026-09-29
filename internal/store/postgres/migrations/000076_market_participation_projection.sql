-- One market authority (Shared control-plane/v1 market-lifecycle.yaml
-- participation). The registry (000070) is authoritative; market.market
-- (000006) becomes its per-country participation projection, the key
-- assignments and trade lanes reference. A country is covered by every
-- registry market naming it as default_country or listing it in countries;
-- it is available for participation while a covering market is in an
-- available status. The projection is written only by market activation.

-- Coverage is derived from the registry, never stored beside it.
CREATE VIEW market.country_coverage AS
SELECT c.country_code,
       r.market_id AS registry_market_id,
       r.status,
       r.activated_at,
       (r.configuration->>'default_country') IS NOT DISTINCT FROM c.country_code AS is_default_country
FROM market.registry r
CROSS JOIN LATERAL (
    SELECT r.configuration->>'default_country' AS country_code
    WHERE jsonb_typeof(r.configuration->'default_country') = 'string'
    UNION
    SELECT jsonb_array_elements_text(r.configuration->'countries')
    WHERE jsonb_typeof(r.configuration->'countries') = 'array'
) c;

-- The country's primary market, whose attributes the projection carries:
-- the earliest-activated available market whose default_country is the
-- country, else the earliest-activated available market listing it.
ALTER TABLE market.market
    ADD COLUMN registry_market_id text REFERENCES market.registry(market_id);

-- Country rows no available registry market covers: rows created before
-- the registry was authoritative, or whose markets have left an available
-- status. They stay readable for existing assignments, but provisioning no
-- longer plans participation in them. ACTIVE mirrors participation
-- available_statuses; the Control Plane's tests hold the two equal.
CREATE VIEW market.uncovered_country_market AS
SELECT m.market_id, m.code, m.name, m.is_active, m.registry_market_id
FROM market.market m
WHERE NOT EXISTS (
    SELECT 1 FROM market.country_coverage c
    WHERE c.country_code = m.code AND c.status = 'ACTIVE'
);
