// Command qualify-founding-onboarding is a READ-ONLY PEO-04/LA-06
// operator gate. It reports current governed Organisation/Tenant/evidence
// posture for Shared first-party entries, without seeding production facts.
package main

import (
 "context"
 "encoding/json"
 "errors"
 "flag"
 "log/slog"
 "os"
 "time"

 "github.com/baobab-platform/baobab-cp/internal/service/organisation"
 "github.com/jackc/pgx/v5/pgxpool"
)

func main(){
 cfg:=flag.String("registry","/baobab/contracts/legal-entity/registry.yaml","pinned Shared first-party registry path")
 flag.Parse()
 if len(flag.Args())!=0{ slog.Error("unexpected operands");os.Exit(1)}
 dsn:=os.Getenv("DATABASE_URL")
 if dsn==""{slog.Error("DATABASE_URL required");os.Exit(1)}
 reg,err:=organisation.LoadFirstPartyRegistry(*cfg)
 if err!=nil{slog.Error("read governed first-party contract","error",err);os.Exit(1)}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second)
 defer cancel()
 pool,err:=pgxpool.New(ctx,dsn)
 if err!=nil{slog.Error("database unavailable","error",err);os.Exit(1)}
 defer pool.Close()
 rows,err:=organisation.QualifyFoundingRoster(ctx,reg,organisation.PostgresFoundingSnapshot{Pool:pool},time.Now().UTC())
 if err!=nil{slog.Error("fail-closed founding qualification","error",err);os.Exit(1)}
 enc:=json.NewEncoder(os.Stdout);enc.SetIndent("","  ")
 if err=enc.Encode(struct{
  RegistryDigest string `json:"registry_digest"`
  EvidenceClass string `json:"evidence_class"`
  Entries []organisation.FoundingQualification `json:"entries"`
 }{reg.Digest,"NON_AUTHORISING_READ_ONLY",rows});err!=nil{
  slog.Error("write qualification report","error",err);os.Exit(1)
 }
 complete:=len(rows)>0
 for _,v:=range rows{complete=complete&&v.Readiness=="IDENTITY_AND_TENANT_GATES_SATISFIED_NOT_PRODUCTION"}
 if !complete{os.Exit(2)}
 // Passing checks permit continued controlled integration. They NEVER
 // grant real CIPC/ownership, permits, IAM access, provider or go-live.
 _=errors.Is // keep ordinary runtime errors redacted by caller
}
