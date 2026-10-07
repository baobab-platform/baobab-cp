package erpprovisioning

import (
	"context"
	"errors"

	"github.com/baobab-platform/baobab-cp/internal/eventingress"
)

// ProvisioningChangedEvent is the event ERP publishes for every committed revision of a provisioning command (Shared erp/v1
// AsyncAPI 1.1.0), accepted over signed delivery (control-plane/v1 event-ingress.yaml).
const ProvisioningChangedEvent = "com.baobab-platform.erp.provisioning.changed.v1"

// ApplyEvent is the processor's handler for provisioning.changed: it hands the event's data to OnProvisioningChanged, which validates
// it, ties it to the submission this Control Plane recorded and applies it only if newer.
//
// An event for an operation not yet recorded (ErrUnknownOperation) is not permanent: ERP may publish while the Control Plane is still
// recording its own answer, so the processor keeps it and tries again. A state that disagrees with the submission can never become
// agreeable, so it is dead-lettered at once.
func (w Worker) ApplyEvent(ctx context.Context, data []byte) error {
	_, _, err := w.OnProvisioningChanged(ctx, data)
	if errors.Is(err, ErrStateDisagrees) {
		return eventingress.Permanent(err)
	}
	return err
}
