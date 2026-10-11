// Command reconcile-founding-evidence performs READ-ONLY Nabhold/Thamani
// PEO-T20 + LA-T27 source reconciliation. No production authority is granted.
// Its inputs MUST be provisioned by an independently controlled operator,
// never submitted from an applicant HTTP route.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service/organisation"
	"github.com/jackc/pgx/v5/pgxpool"
	"gopkg.in/yaml.v3"
)

type firstPartyRow struct {
	ID string `yaml:"id"`
	IdentityClass string `yaml:"identity_class"`
	IncorporationClaim string `yaml:"incorporation_claim"`
	RegistrationIdentifier string `yaml:"registration_identifier"`
}
type trustFile struct {
	IssuerID string `json:"issuer_id"`
	SignerID string `json:"signer_id"`
	PublicKeyHex string `json:"public_key_hex"`
	ProposedBy string `json:"proposed_by"`
	ApprovedBy string `json:"approved_by"`
	ApprovalRecord string `json:"approval_record"`
	ApprovedAt time.Time `json:"approved_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// readRestrictedFile requires an operator-owned regular file with no
// group/other write access. Refuse symlinks, unbounded contents and paths
// borrowed from public upload/document storage.
func readRestrictedFile(path string, maxBytes int64) ([]byte,error) {
	if !filepath.IsAbs(path) || maxBytes<=0 {return nil,errors.New("operator-approved absolute path required")}
	info,err:=os.Lstat(path)
	if err!=nil{return nil,err}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink!=0 ||
		info.Mode().Perm()&0022 !=0 || info.Size()>maxBytes || info.Size()<=0 {
		return nil,errors.New("unsafe or oversized operator configuration")
	}
	return os.ReadFile(path)
}

func loadDeclaredFounders(path string) (organisation.FoundingIdentityInput,organisation.FoundingIdentityInput,error){
	var empty organisation.FoundingIdentityInput
	raw,err:=readRestrictedFile(path,1<<20)
	if err!=nil{return empty,empty,err}
	if _,err=organisation.ParseFirstPartyRegistry(raw);err!=nil{return empty,empty,err}
	var cfg struct {Entities []firstPartyRow `yaml:"entities"`}
	if err=yaml.Unmarshal(raw,&cfg);err!=nil{return empty,empty,err}
	var parent,child organisation.FoundingIdentityInput
	for _,row:=range cfg.Entities{
		if row.ID!="NABHOLD"&&row.ID!="THAMANI-GLOBAL"{continue}
		if row.IdentityClass!="LEGAL_PERSON"||row.IncorporationClaim!="REGISTERED_EVIDENCED"||
			strings.TrimSpace(row.RegistrationIdentifier)==""{
			return empty,empty,errors.New("Shared declarations do not provide both independently claimable legal persons")
		}
		item:=organisation.FoundingIdentityInput{RegistryID:row.ID,DeclaredRegistrationNumber:row.RegistrationIdentifier}
		if row.ID=="NABHOLD"{parent=item}else{child=item}
	}
	if parent.RegistryID==""||child.RegistryID==""{return empty,empty,errors.New("both founding company declarations required")}
	return parent,child,nil
}

func loadTrust(path string)(map[string]organisation.IndependentControlTrust,error){
	raw,err:=readRestrictedFile(path,64*1024)
	if err!=nil{return nil,err}
	var entries []trustFile
	if err=json.Unmarshal(raw,&entries);err!=nil{return nil,err}
	if len(entries)==0||len(entries)>16{return nil,errors.New("independent signer trust set invalid")}
	out:=make(map[string]organisation.IndependentControlTrust,len(entries))
	for _,x:=range entries{
		public,decodeErr:=hex.DecodeString(x.PublicKeyHex)
		if decodeErr!=nil||len(public)!=ed25519.PublicKeySize||x.SignerID==""||x.IssuerID==""||
			x.ProposedBy==""||x.ApprovedBy==""||x.ProposedBy==x.ApprovedBy||
			x.ApprovalRecord==""||x.ApprovedAt.IsZero()||x.ExpiresAt.IsZero(){
			return nil,errors.New("independent source signer not approved")
		}
		if _,duplicate:=out[x.SignerID];duplicate{return nil,errors.New("duplicate approved external signer ID")}
		out[x.SignerID]=organisation.IndependentControlTrust{
			IssuerID:x.IssuerID,SignerID:x.SignerID,PublicKey:ed25519.PublicKey(public),
			ProposedBy:x.ProposedBy,ApprovedBy:x.ApprovedBy,
			ApprovalRecord:x.ApprovalRecord,ApprovedAt:x.ApprovedAt,ExpiresAt:x.ExpiresAt,
		}
	}
	return out,nil
}

func run(ctx context.Context,args []string)(organisation.FoundingCompanyReconciliation,error){
	var empty organisation.FoundingCompanyReconciliation
	flags:=flag.NewFlagSet("reconcile-founding-evidence",flag.ContinueOnError)
	registryPath:=flags.String("registry","","absolute path to pinned Shared legal-entity registry")
	trustPath:=flags.String("control-trust","","absolute path to independently approved external signer trust")
	parentOrg:=flags.String("nabhold-organisation","","Nabhold canonical Organisation ID")
	childOrg:=flags.String("thamani-organisation","","Thamani canonical Organisation ID")
	parentCase:=flags.String("nabhold-case","","Nabhold current CIPC OEV-03 VerificationCase ID")
	childCase:=flags.String("thamani-case","","Thamani current CIPC OEV-03 VerificationCase ID")
	if err:=flags.Parse(args);err!=nil{return empty,err}
	if len(flags.Args())!=0||*parentOrg==""||*childOrg==""||*parentCase==""||*childCase==""{
		return empty,errors.New("both exact organisation/case pairs are required")
	}
	parent,child,err:=loadDeclaredFounders(*registryPath)
	if err!=nil{return empty,err}
	approved,err:=loadTrust(*trustPath)
	if err!=nil{return empty,err}
	parent.OrganisationID=*parentOrg;child.OrganisationID=*childOrg
	parent.VerificationCaseID=*parentCase;child.VerificationCaseID=*childCase
	dsn:=os.Getenv("DATABASE_URL")
	if dsn==""{return empty,errors.New("DATABASE_URL is required")}
	repo,err:=repository.Open(ctx,dsn)
	if err!=nil{return empty,err}
	defer repo.Close()
	pool,err:=pgxpool.New(ctx,dsn)
	if err!=nil{return empty,err}
	defer pool.Close()
	now:=time.Now
	reconciler:=organisation.FoundingEvidenceReconciler{
		Registrations:repo,Corporate:repo,
		Proofs:organisation.PostgresControlProofStore{Pool:pool,Trust:approved,Now:now},
		Trust:approved,Now:now,
	}
	return reconciler.Reconcile(ctx,parent,child)
}

func main(){
	ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second)
	defer cancel()
	report,err:=run(ctx,os.Args[1:])
	if err!=nil{
		slog.Error("read-only founding verification unavailable","reason",err)
		os.Exit(1)
	}
	out:=json.NewEncoder(os.Stdout)
	out.SetIndent("","  ")
	if err:=out.Encode(report);err!=nil{fmt.Fprintln(os.Stderr,"report write denied");os.Exit(1)}
	if report.Standing!="BOTH_TRACKS_VERIFIED"{
		// This is an evidence-preparation report, not a production go-live
		// approval: nonzero status prevents CI from using pending as green.
		os.Exit(2)
	}
}
