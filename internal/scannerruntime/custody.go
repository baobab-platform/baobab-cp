package scannerruntime

import (
	"context"
	"reflect"
	"time"

	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/baobab-platform/baobab-cp/internal/documentcustody"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// CustodyConfiguration contains deployment authority, not application input.
// Its policy file is re-read at every seal/maintenance operation. Region and
// role approval cannot be inferred from the evidence's tenant or a default.
type CustodyConfiguration struct {
	Workload          string                 `json:"workload"`
	AWS               documentcustody.Config `json:"aws"`
	PolicyFile        string                 `json:"policy_file"`
	ProposedBy        string                 `json:"proposed_by"`
	ApprovedBy        string                 `json:"approved_by"`
	ApprovalReference string                 `json:"approval_reference"`
	EffectiveFrom     time.Time              `json:"effective_from"`
	ExpiresAt         time.Time              `json:"expires_at"`
}
type ApprovedRetentionPolicy struct {
	Current bool `json:"current"`
	repository.RetentionPolicy
	ProposedBy        string    `json:"proposed_by"`
	ApprovedBy        string    `json:"approved_by"`
	ApprovalReference string    `json:"approval_reference"`
	EffectiveFrom     time.Time `json:"effective_from"`
	ExpiresAt         time.Time `json:"expires_at"`
}
type MountedCustodyPolicies struct {
	Path string
	Now  func() time.Time
}

func (m MountedCustodyPolicies) CustodyPolicy(ctx context.Context, id, version string) (repository.RetentionPolicy, error) {
	var empty repository.RetentionPolicy
	if ctx == nil || ctx.Err() != nil || m.Now == nil {
		return empty, ErrConfiguration
	}
	var file struct {
		Policies []ApprovedRetentionPolicy `json:"policies"`
	}
	if privateJSON(m.Path, &file) != nil || len(file.Policies) == 0 || len(file.Policies) > 100 {
		return empty, ErrConfiguration
	}
	seen := map[string]bool{}
	currentIDs := map[string]bool{}
	var selected repository.RetentionPolicy
	for _, p := range file.Policies {
		if !p.Valid() || seen[p.ID+"/"+p.Version] || p.Current && currentIDs[p.ID] || !currentApproval(p.ProposedBy, p.ApprovedBy, p.ApprovalReference, p.EffectiveFrom, p.ExpiresAt, m.Now()) {
			return empty, ErrConfiguration
		}
		seen[p.ID+"/"+p.Version] = true
		if p.Current {
			currentIDs[p.ID] = true
		}
		if p.ID == id && (version != "" && p.Version == version || version == "" && p.Current) {
			selected = p.RetentionPolicy
		}
	}
	if selected.ID == "" {
		return empty, ErrConfiguration
	}
	return selected, nil
}
func currentApproval(maker, checker, reference string, start, end, now time.Time) bool {
	return maker != "" && checker != "" && maker != checker && reference != "" && !start.IsZero() && !start.After(now) && end.After(now)
}

type privateIdentityToken struct{ path string }

var _ stscreds.IdentityTokenRetriever = privateIdentityToken{}

func (p privateIdentityToken) GetIdentityToken() ([]byte, error) { return privateFile(p.path, 32768) }

func ComposeCustody(ctx context.Context, file string, repo *repository.PostgresRepository) (repository.DurableDocumentVault, CustodyConfiguration, error) {
	var cfg CustodyConfiguration
	var empty repository.DurableDocumentVault
	if ctx == nil || ctx.Err() != nil || repo == nil || privateJSON(file, &cfg) != nil || cfg.AWS.Validate() != nil || (cfg.Workload != "SCANNER_INTAKE" && cfg.Workload != "CUSTODY_MAINTENANCE") || !currentApproval(cfg.ProposedBy, cfg.ApprovedBy, cfg.ApprovalReference, cfg.EffectiveFrom, cfg.ExpiresAt, time.Now()) {
		return empty, cfg, ErrConfiguration
	}
	// Read the token once for startup validation; STS rereads the private mount
	// on refresh, preserving short-lived projected workload credentials.
	if _, err := privateFile(cfg.AWS.IdentityTokenFile, 32768); err != nil {
		return empty, cfg, ErrConfiguration
	}
	policies := MountedCustodyPolicies{Path: cfg.PolicyFile, Now: time.Now}
	var list struct {
		Policies []ApprovedRetentionPolicy `json:"policies"`
	}
	if privateJSON(cfg.PolicyFile, &list) != nil || len(list.Policies) == 0 {
		return empty, cfg, ErrConfiguration
	}
	if _, err := policies.CustodyPolicy(ctx, list.Policies[0].ID, list.Policies[0].Version); err != nil {
		return empty, cfg, ErrConfiguration
	}
	authorize := func(c context.Context) error {
		var current CustodyConfiguration
		if c == nil || c.Err() != nil || privateJSON(file, &current) != nil || !reflect.DeepEqual(current, cfg) || !currentApproval(current.ProposedBy, current.ApprovedBy, current.ApprovalReference, current.EffectiveFrom, current.ExpiresAt, time.Now()) {
			return ErrConfiguration
		}
		return nil
	}
	backend, err := documentcustody.New(cfg.AWS, privateIdentityToken{cfg.AWS.IdentityTokenFile}, authorize)
	if err != nil || backend.Validate(ctx) != nil {
		return empty, cfg, ErrConfiguration
	}
	return repository.DurableDocumentVault{Repository: repo, Backend: backend, Policies: policies, Prefix: cfg.AWS.Prefix, Now: time.Now}, cfg, nil
}

// RunCustodyMaintenance is deliberately report-only without an approved
// snapshot mount. It has no endpoint accepting applicant maintenance input.
func RunCustodyMaintenance(ctx context.Context, config, approval, afterEvidence, afterArtifact string, repo *repository.PostgresRepository, correlation string) ([]repository.CustodyReport, error) {
	vault, cfg, err := ComposeCustody(ctx, config, repo)
	if err != nil || cfg.Workload != "CUSTODY_MAINTENANCE" {
		return nil, ErrConfiguration
	}
	if approval == "" {
		return vault.Reports(ctx, afterEvidence, afterArtifact, 100)
	}
	var job repository.CustodyApproval
	if privateJSON(approval, &job) != nil || !job.Valid(time.Now()) {
		return nil, ErrConfiguration
	}
	receipt, err := vault.Maintain(ctx, job, repository.AuditActor{ActorID: cfg.AWS.RoleARN, ActorType: "workload", ClientID: "baobab-document-custody-maintenance", CorrelationID: correlation})
	if err != nil {
		return nil, ErrConfiguration
	}
	return []repository.CustodyReport{receipt}, nil
}
