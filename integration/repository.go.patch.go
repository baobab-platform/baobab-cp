// Target path: internal/repository/repository.go
package integration

/*
No new resolver repository abstraction is required for the basic ZB-02 slice:
the existing CapabilityRepository already exposes:
    ListBindings(ctx, capabilityKey)
    ListActiveInstances(ctx, engineID)

and CapabilityWriter already exposes:
    CreateBinding(...)
    SaveBinding(...)

The provisioning service composes those accepted interfaces with:
    CapabilityRegistryRepository
    CapabilityScopeWriter

If desired, define a convenience interface only:

type CapabilityBindingProvisioningRepository interface {
    CapabilityRepository
    CapabilityWriter
    CapabilityRegistryRepository
    CapabilityScopeWriter
}

Do not duplicate repository methods under new names.
*/
