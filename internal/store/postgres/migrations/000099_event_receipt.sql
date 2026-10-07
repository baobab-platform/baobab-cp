-- Signed event delivery: the durable receipt of every engine event the Control Plane accepted (Shared control-plane/v1
-- receiveEngineEvent, FB-04c).
--
-- Delivery is at-least-once, so the receiving side must be an idempotent inbox. A receipt is keyed by the envelope's
-- (source, id), the delivery identity the canonical envelope defines, and holds the exact bytes received with their digest, so a
-- redelivery of the same event is recognised (200 DUPLICATE) and the same identity arriving with other content is a conflict (409),
-- never a second application. The row is written before the sender is answered: a 2xx means "recorded", never "acted on".
-- Applying the event is a separate, retried step (status, attempts, next_attempt_at); an event for something the Control Plane does
-- not know yet stays pending until it does, or until its policy age elapses and it is dead-lettered for operators.
--
-- Receipts are kept at least seven days (longer than any sender's 72 hour retry horizon), so a late retry is a duplicate and not
-- a new event. messaging.inbox from 000015 was never used, is keyed by event id alone and holds no body; it is left as it is.
CREATE TABLE messaging.event_receipt (
    source        text NOT NULL,
    event_id      uuid NOT NULL,
    event_type    text NOT NULL,
    tenant_id     text NOT NULL,
    key_id        text NOT NULL,
    body_sha256   text NOT NULL CHECK (body_sha256 ~ '^[0-9a-f]{64}$'),
    body          bytea NOT NULL CHECK (octet_length(body) BETWEEN 1 AND 1048576),
    received_at   timestamptz NOT NULL DEFAULT now(),
    status        text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'applied', 'dead_letter')),
    attempts      integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at timestamptz,
    last_error    text,
    processed_at  timestamptz,
    PRIMARY KEY (source, event_id)
);

CREATE INDEX event_receipt_due_idx ON messaging.event_receipt (next_attempt_at, received_at) WHERE status = 'pending';
CREATE INDEX event_receipt_retention_idx ON messaging.event_receipt (received_at) WHERE status <> 'pending';

-- What was received is fixed: only the processing state moves.
CREATE FUNCTION messaging.event_receipt_fixed() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.source <> OLD.source OR NEW.event_id <> OLD.event_id OR NEW.event_type <> OLD.event_type
       OR NEW.tenant_id <> OLD.tenant_id OR NEW.key_id <> OLD.key_id OR NEW.body_sha256 <> OLD.body_sha256
       OR NEW.body <> OLD.body OR NEW.received_at <> OLD.received_at THEN
        RAISE EXCEPTION 'an event receipt is fixed once recorded; only its processing state advances';
    END IF;
    IF OLD.status <> 'pending' AND NEW.status <> OLD.status THEN
        RAISE EXCEPTION 'a processed event receipt does not return to pending';
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER event_receipt_fixed
    BEFORE UPDATE ON messaging.event_receipt
    FOR EACH ROW EXECUTE FUNCTION messaging.event_receipt_fixed();
