package api

import (
 "net/http/httptest"
 "testing"
)

func TestCX05KStrictCommandsRejectUntrustedActorAndRoutes(t *testing.T){
 tests:=[]string{
  `{"kind":"REDACTED_CSV_EXPORT","target_ids":[],"principal_id":"someone"}`,
  `{"kind":"REDACTED_CSV_EXPORT","target_ids":[]} {"kind":"BULK_METADATA_PROPOSAL"}`,
  `{"kind":"REDACTED_CSV_EXPORT","target_ids":[],"authority":{"organisation":"foreign"}}`,
 }
 for _,raw:=range tests {
  req:=httptest.NewRequest("POST","/v1/admin/datasets/preview",nil)
  rec:=httptest.NewRecorder()
  var body consolePreviewInput
  if strictDatasetBody(rec,req,[]byte(raw),&body){
   t.Fatalf("accepted untrusted body: %s",raw)
  }
  if rec.Code!=400{t.Fatalf("expected 400, got %d",rec.Code)}
 }
}
func TestCX05KProjectionMapsVersionedMetadataOnly(t *testing.T){
 changes:=consoleChanges([]consoleChangeInput{{OrganisationID:"0199d000-1111-7777-8000-111111111111",ExpectedVersion:7,InternalLabel:"review"}})
 if len(changes)!=1||changes[0].ExpectedVersion!=7||changes[0].InternalLabel!="review"{t.Fatal("bulk mapping changed")}
}
