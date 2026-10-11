package authority

// DocumentCandidateAttempt is a non-authorising ledger projection shared by
// the private verification worker and repository adapter. The evidence ledger
// must persist candidate values in encrypted custody, not open metadata.
type DocumentCandidateAttempt struct {
	CaseID     string
	ClaimID    string
	EvidenceID string
	ArtifactID string
	ActorID    string
	Receipt    DocumentExtractionReceipt
}
