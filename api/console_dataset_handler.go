package api

// CX-05K: nonproduction-only, authenticated dataset commands. No browser
// decides the maker/checker or carries delegated organisation authority.
import (
 "context"
 "bytes"
 "encoding/json"
 "io"
 "net/http"
 "strings"
 "time"

 "github.com/baobab-platform/baobab-cp/internal/administration"
 "github.com/baobab-platform/baobab-cp/internal/domain"
 "github.com/baobab-platform/baobab-cp/internal/repository"
 "github.com/go-chi/chi/v5"
)

type consoleDatasetStore interface {
 SnapshotConsoleDataset(context.Context,[]string)([]domain.ConsoleDatasetRecord,error)
 ProposeConsoleDataset(context.Context,domain.ConsoleDatasetIntent,[]domain.ConsoleMetadataChange,repository.AuditActor,repository.ConsoleDatasetPermission,time.Time)(repository.ConsoleDatasetResult,error)
 ApproveConsoleDataset(context.Context,string,string,repository.AuditActor,repository.ConsoleDatasetPermission,time.Time)(repository.ConsoleDatasetResult,error)
 ExecuteConsoleDataset(context.Context,string,repository.AuditActor,repository.ConsoleDatasetPermission,time.Time)(repository.ConsoleDatasetResult,error)
 ReadConsoleDatasetExport(context.Context,string,repository.AuditActor,repository.ConsoleDatasetPermission,time.Time)([]byte,error)
 GetConsoleDatasetDescriptor(context.Context,string)(repository.ConsoleDatasetDescriptor,error)
}
type consoleDatasetHandler struct {
 repo consoleDatasetStore
 grants repository.AdministrativeGrantReader
 identities repository.IdentityRepository
 environment string
 now func()time.Time
}
type consoleChangeInput struct {
 OrganisationID string `json:"organisation_id"`
 ExpectedVersion int64 `json:"expected_version"`
 InternalLabel string `json:"internal_label"`
}
type consolePreviewInput struct {
 Kind string `json:"kind"`
 TargetIDs []string `json:"target_ids"`
 Changes []consoleChangeInput `json:"changes"`
}
type consoleProposalInput struct {
 Kind string `json:"kind"`
 TargetIDs []string `json:"target_ids"`
 Changes []consoleChangeInput `json:"changes"`
 SourceFingerprint string `json:"source_fingerprint"`
 Purpose string `json:"purpose"`
 RecipientReference string `json:"recipient_reference"`
 ValidUntil time.Time `json:"valid_until"`
}
type consoleApprovalInput struct {EvidenceReference string `json:"evidence_reference"`}

// The published Shared strict schema is the authority. Until the full
// Shared OpenAPI client lock is synchronised, this endpoint repeats its
// closed DTO grammar and rejects every unknown field and trailing document.
func strictDatasetBody(w http.ResponseWriter,r *http.Request,raw []byte,out any)bool{
 decoder:=json.NewDecoder(bytes.NewReader(raw))
 decoder.DisallowUnknownFields()
 if err:=decoder.Decode(out);err!=nil{
  problem(w,r,http.StatusBadRequest,"INVALID_DATASET_COMMAND","invalid dataset command fields",false);return false
 }
 var extra any
 if err:=decoder.Decode(&extra);err!=io.EOF{
  problem(w,r,http.StatusBadRequest,"INVALID_DATASET_COMMAND","invalid trailing dataset payload",false);return false
 }
 return true
}
func (h consoleDatasetHandler) clock()time.Time {
 if h.now!=nil{return h.now()}
 return time.Now().UTC()
}
func (h consoleDatasetHandler) failure(w http.ResponseWriter,r *http.Request,err error){
 // All failure states are intentionally indistinguishable externally:
 // no existence oracle for organisations, proposed plans or privileges.
 problem(w,r,http.StatusNotFound,"DATASET_UNAVAILABLE","governed dataset unavailable",false)
}
func (h consoleDatasetHandler) actor(w http.ResponseWriter,r *http.Request)(string,repository.AuditActor,bool){
 if h.repo==nil||h.identities==nil||h.grants==nil{
  problem(w,r,http.StatusServiceUnavailable,"DATASET_AUTHORITY_UNAVAILABLE","authoritative dataset sources unavailable",true)
  return "",repository.AuditActor{},false
 }
 principal,actor,ok:=resolveActor(w,r,h.identities,false)
 if !ok{return "",repository.AuditActor{},false}
 if actor.ActorType!="human"{
  h.failure(w,r,nil)
  return "",repository.AuditActor{},false
 }
 return principal,actor,true
}
// grantMatrix reloads ACTIVE identity and evaluated permission facts for
// EACH maker/checker on every command. Only EXACT organisation anchors count.
// Corporate group membership, platform role and related tenants add nothing.
func (h consoleDatasetHandler) grantMatrix(ctx context.Context,principals []string,at time.Time)(repository.ConsoleDatasetPermission,error){
 visible:=map[string]bool{}
 seen:=map[string]bool{}
 for _,id:=range principals {
  if id==""||seen[id]{continue}
  seen[id]=true
  identity,err:=h.identities.GetPrincipal(ctx,id)
  if err!=nil||identity.Status!="ACTIVE"{return nil,repository.ErrConsoleDatasetDenied}
  grants,sources,err:=h.grants.AdministrativeGrantsOf(ctx,id)
  if err!=nil{return nil,err}
  scopes:=make([]administration.Scope,0,len(grants)+len(sources))
  for _,g:=range grants{scopes=append(scopes,g.Scope)}
  for _,g:=range sources{scopes=append(scopes,g.Scope)}
  rel,err:=h.grants.EffectiveRelations(ctx,administration.TenantsOf(scopes...),at)
  if err!=nil{return nil,err}
  effective:=administration.Effective(id,grants,sources,at,rel)
  for _,g:=range effective.Grants{
   s:=g.Scope
   if s.Level!=administration.LevelOrganisation||
     (s.Mode!=""&&s.Mode!=administration.ModeExact)||
     (s.Environment!=""&&s.Environment!=h.environment)||
     !domain.IsUUID(s.OrganisationID){continue}
   visible[id+"|"+strings.ToLower(s.OrganisationID)+"|"+g.Permission]=true
  }
 }
 return func(principal,org,permission string)bool{
  return visible[principal+"|"+strings.ToLower(org)+"|"+permission]
 },nil
}
func (h consoleDatasetHandler) permitted(ctx context.Context,ids []string,actor string,
 kind string,at time.Time)(repository.ConsoleDatasetPermission,error){
 allowed,err:=h.grantMatrix(ctx,[]string{actor},at)
 if err!=nil{return nil,err}
 cap:=1000
 if kind==domain.ConsoleBulk{cap=50}
 if len(ids)==0||len(ids)>cap{return nil,repository.ErrConsoleDatasetDenied}
 for _,id:=range ids {
  if !domain.IsUUID(id)||!allowed(actor,id,"organisation.view"){return nil,repository.ErrConsoleDatasetDenied}
  if kind==domain.ConsoleExport&&!allowed(actor,id,"organisation.export"){return nil,repository.ErrConsoleDatasetDenied}
  if kind==domain.ConsoleBulk&&(!allowed(actor,id,"organisation.bulk.propose")||
    !allowed(actor,id,"organisation.manage")){return nil,repository.ErrConsoleDatasetDenied}
 }
 return allowed,nil
}
func consoleChanges(in []consoleChangeInput)[]domain.ConsoleMetadataChange{
 out:=make([]domain.ConsoleMetadataChange,0,len(in))
 for _,v:=range in{out=append(out,domain.ConsoleMetadataChange{
  OrganisationID:v.OrganisationID,ExpectedVersion:v.ExpectedVersion,InternalLabel:v.InternalLabel})}
 return out
}
func consoleReceipt(v repository.ConsoleDatasetResult)map[string]any{
 return map[string]any{"digest":v.Digest,"kind":v.Kind,"state":v.State}
}
func (h consoleDatasetHandler) preview(w http.ResponseWriter,r *http.Request){
 principal,_,ok:=h.actor(w,r);if !ok{return}
 raw,ok:=readBody(w,r);if !ok{return}
 var req consolePreviewInput
 if !strictDatasetBody(w,r,raw,&req){return}
 now:=h.clock()
 if req.Kind!=domain.ConsoleExport&&req.Kind!=domain.ConsoleBulk{h.failure(w,r,nil);return}
 if req.Kind==domain.ConsoleExport&&len(req.Changes)!=0{h.failure(w,r,nil);return}
 allowed,err:=h.permitted(r.Context(),req.TargetIDs,principal,req.Kind,now)
 if err!=nil||allowed==nil{h.failure(w,r,err);return}
 rows,err:=h.repo.SnapshotConsoleDataset(r.Context(),req.TargetIDs)
 if err!=nil||len(rows)!=len(req.TargetIDs){h.failure(w,r,err);return}
 fingerprint,err:=domain.DatasetSourceFingerprint(rows)
 if err!=nil{h.failure(w,r,err);return}
 var changesHash any=nil
 if req.Kind==domain.ConsoleBulk {
  var hash string
  hash,err=domain.BulkChangeFingerprint(consoleChanges(req.Changes))
  if err!=nil||len(req.Changes)!=len(req.TargetIDs){h.failure(w,r,err);return}
  changesHash=hash
 }
 projected:=make([]map[string]any,0,len(rows))
 for _,row:=range rows{
  projected=append(projected,map[string]any{
   "organisation_id":row.OrganisationID,"display_name":row.DisplayName,
   "status":row.Status,"verification_state":row.VerificationState,
   "source_authority":row.SourceAuthority,"jurisdiction":row.Jurisdiction,
   "revision":row.Revision,
  })
 }
 w.Header().Set("Cache-Control","private, no-store")
 writeJSON(w,http.StatusOK,map[string]any{"kind":req.Kind,"target_ids":req.TargetIDs,
  "changes":req.Changes,"source_fingerprint":fingerprint,"change_fingerprint":changesHash,"rows":projected})
}
func (h consoleDatasetHandler) propose(w http.ResponseWriter,r *http.Request){
 principal,actor,ok:=h.actor(w,r);if !ok{return}
 key,ok:=provisioningIdempotencyKey(w,r);if !ok{return}
 raw,ok:=readBody(w,r);if !ok{return}
 var req consoleProposalInput
 if !strictDatasetBody(w,r,raw,&req){return}
 now:=h.clock()
 allowed,err:=h.permitted(r.Context(),req.TargetIDs,principal,req.Kind,now)
 if err!=nil{h.failure(w,r,err);return}
 changes:=consoleChanges(req.Changes)
 intent:=domain.ConsoleDatasetIntent{Kind:req.Kind,MakerPrincipalID:principal,
  Purpose:req.Purpose,RecipientReference:req.RecipientReference,IdempotencyKey:key,
  SourceFingerprint:req.SourceFingerprint,TargetIDs:req.TargetIDs,ValidUntil:req.ValidUntil}
 if req.Kind==domain.ConsoleBulk{
  intent.Action="organisation.metadata.update"
  intent.ChangeFingerprint,err=domain.BulkChangeFingerprint(changes)
  if err!=nil{h.failure(w,r,err);return}
 }
 result,err:=h.repo.ProposeConsoleDataset(r.Context(),intent,changes,actor,allowed,now)
 if err!=nil{h.failure(w,r,err);return}
 w.Header().Set("Cache-Control","no-store")
 w.Header().Set("Location","/v1/admin/datasets/"+result.Digest)
 writeJSON(w,http.StatusCreated,consoleReceipt(result))
}
func (h consoleDatasetHandler) descriptor(w http.ResponseWriter,r *http.Request)(repository.ConsoleDatasetDescriptor,string,repository.AuditActor,repository.ConsoleDatasetPermission,bool){
 principal,actor,ok:=h.actor(w,r);if !ok{return repository.ConsoleDatasetDescriptor{},"",repository.AuditActor{},nil,false}
 digest:=chi.URLParam(r,"digest")
 description,err:=h.repo.GetConsoleDatasetDescriptor(r.Context(),digest)
 if err!=nil{h.failure(w,r,err);return repository.ConsoleDatasetDescriptor{},"",repository.AuditActor{},nil,false}
 if principal!=description.Intent.MakerPrincipalID && principal!=description.CheckerID &&
   strings.HasSuffix(r.URL.Path,"/approve")==false {h.failure(w,r,nil);return repository.ConsoleDatasetDescriptor{},"",repository.AuditActor{},nil,false}
 if principal==description.Intent.MakerPrincipalID && strings.HasSuffix(r.URL.Path,"/approve"){
  h.failure(w,r,nil);return repository.ConsoleDatasetDescriptor{},"",repository.AuditActor{},nil,false
 }
 all,err:=h.grantMatrix(r.Context(),[]string{principal,description.Intent.MakerPrincipalID,description.CheckerID},h.clock())
 if err!=nil{h.failure(w,r,err);return repository.ConsoleDatasetDescriptor{},"",repository.AuditActor{},nil,false}
 for _,id:=range description.Intent.TargetIDs{
  if !all(principal,id,"organisation.view")||
    (description.Intent.Kind==domain.ConsoleExport&&!all(principal,id,"organisation.export"))||
    (description.Intent.Kind==domain.ConsoleBulk&&principal==description.Intent.MakerPrincipalID&&
       !all(principal,id,"organisation.bulk.propose"))||
    (description.Intent.Kind==domain.ConsoleBulk&&principal!=description.Intent.MakerPrincipalID&&
       !all(principal,id,"organisation.bulk.approve")) {
   h.failure(w,r,nil);return repository.ConsoleDatasetDescriptor{},"",repository.AuditActor{},nil,false
  }
 }
 return description,principal,actor,all,true
}
func (h consoleDatasetHandler) get(w http.ResponseWriter,r *http.Request){
 desc,_,_,_,ok:=h.descriptor(w,r);if !ok{return}
 w.Header().Set("Cache-Control","no-store")
 writeJSON(w,http.StatusOK,map[string]any{"digest":chi.URLParam(r,"digest"),"kind":desc.Intent.Kind,"state":desc.State})
}
func (h consoleDatasetHandler) approve(w http.ResponseWriter,r *http.Request){
 desc,_,actor,allowed,ok:=h.descriptor(w,r);if !ok{return}
 if _,ok=provisioningIdempotencyKey(w,r);!ok{return}
 if desc.State!="PENDING_APPROVAL"{h.failure(w,r,nil);return}
 // Current maker and checker permissions are tested inside the domain guard.
 raw,ok:=readBody(w,r);if !ok{return}
 var req consoleApprovalInput
 if !strictDatasetBody(w,r,raw,&req){return}
 result,err:=h.repo.ApproveConsoleDataset(r.Context(),chi.URLParam(r,"digest"),req.EvidenceReference,actor,allowed,h.clock())
 if err!=nil{h.failure(w,r,err);return}
 writeJSON(w,http.StatusOK,consoleReceipt(result))
}
func (h consoleDatasetHandler) execute(w http.ResponseWriter,r *http.Request){
 _,principal,actor,allowed,ok:=h.descriptor(w,r);if !ok{return}
 if principal==""{h.failure(w,r,nil);return}
 if _,ok=provisioningIdempotencyKey(w,r);!ok{return}
 result,err:=h.repo.ExecuteConsoleDataset(r.Context(),chi.URLParam(r,"digest"),actor,allowed,h.clock())
 if err!=nil{h.failure(w,r,err);return}
 writeJSON(w,http.StatusOK,consoleReceipt(result))
}
func (h consoleDatasetHandler) download(w http.ResponseWriter,r *http.Request){
 _,_,actor,allowed,ok:=h.descriptor(w,r);if !ok{return}
 blob,err:=h.repo.ReadConsoleDatasetExport(r.Context(),chi.URLParam(r,"digest"),actor,allowed,h.clock())
 if err!=nil{h.failure(w,r,err);return}
 w.Header().Set("Cache-Control","private, no-store")
 w.Header().Set("X-Content-Type-Options","nosniff")
 w.Header().Set("Content-Security-Policy","sandbox")
 w.Header().Set("Content-Disposition","attachment; filename=\"redacted-organisations.csv\"")
 w.Header().Set("Content-Type","text/csv; charset=utf-8")
 w.WriteHeader(http.StatusOK)
 _,_=w.Write(blob)
}
