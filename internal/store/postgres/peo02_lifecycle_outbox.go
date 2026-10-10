// PEO-02C canonical founding lifecycle notifications.
// Only Control Plane writes these facts; an event is an invalidation hint,
// never a transferable sponsorship, identity proof or entitlement.
package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/events"
	basestore "github.com/baobab-platform/baobab-cp/internal/store"
	"github.com/jackc/pgx/v5"
)

const foundingEventSource = "urn:baobab-platform:service:baobab-cp"
const foundingEventSchema = "https://contracts.baobab-platform.com/admission/v2/founding-lifecycle-events.schema.json"

func publishFoundingLifecycle(ctx context.Context, tx pgx.Tx, meta basestore.RequestMetadata,
	kind, grantID, organisationID, sponsorshipID, status string) error {
	var typ, definition string
	var data map[string]any
	switch kind + "/" + status {
	case "SPONSORSHIP/ACTIVE":
		typ, definition = "com.baobab-platform.control-plane.founding-sponsorship.activated.v1", "SponsorshipActivated"
	case "SPONSORSHIP/SUSPENDED":
		typ, definition = "com.baobab-platform.control-plane.founding-sponsorship.suspended.v1", "SponsorshipSuspended"
	case "SPONSORSHIP/REVOKED":
		typ, definition = "com.baobab-platform.control-plane.founding-sponsorship.revoked.v1", "SponsorshipRevoked"
	case "DOCUMENTARY_DEFERRAL/ACTIVE":
		typ, definition = "com.baobab-platform.control-plane.founding-documentary-deferral.activated.v1", "DeferralActivated"
	case "DOCUMENTARY_DEFERRAL/REVOKED":
		typ, definition = "com.baobab-platform.control-plane.founding-documentary-deferral.revoked.v1", "DeferralRevoked"
	default:
		return ErrFoundingAuthority
	}
	if kind == "SPONSORSHIP" {
		data = map[string]any{"sponsorship_id": grantID, "operating_organisation_id": organisationID, "status": status}
	} else {
		data = map[string]any{"deferral_id": grantID, "organisation_id": organisationID, "status": status}
		// Shared's DeferralActivated/Revoked payload carries only immutable
		// deferral id, organisation id and status; sponsorship is resolved
		// from CP's own current authoritative record by the consumer.
		_ = sponsorshipID
	}
	env, err := events.New(events.Params{
		Type: typ, Source: foundingEventSource,
		Subject: "founding-governance/" + grantID,
		DataSchema: foundingEventSchema + "#/$defs/" + definition,
		CorrelationID: meta.CorrelationID, Data: data,
	})
	if err != nil {
		return fmt.Errorf("construct founding lifecycle event: %w", err)
	}
	raw, err := json.Marshal(env)
	if err != nil {
		return err
	}
	aggregate := "founding_group_sponsorship"
	if kind == "DOCUMENTARY_DEFERRAL" { aggregate = "founding_documentary_deferral" }
	_, err = tx.Exec(ctx, `INSERT INTO messaging.outbox
	 (aggregate_type,aggregate_id,aggregate_version,event_type,tenant_id,correlation_id,payload)
	 VALUES($1,$2,$3,$4,NULL,$5::uuid,$6::jsonb)`,
		aggregate, grantID, time.Now().UTC().UnixMicro(), typ, meta.CorrelationID, raw)
	return err
}
