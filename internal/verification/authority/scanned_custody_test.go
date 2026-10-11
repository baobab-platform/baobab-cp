package authority

import (
 "bytes"
 "context"
 "crypto/ed25519"
 "crypto/rand"
 "crypto/sha256"
 "encoding/hex"
 "errors"
 "os"
 "path/filepath"
 "testing"
 "time"
)

func TestPEO03KSignedScanProofAndSealedArtifact(t *testing.T) {
 public,private,err:=ed25519.GenerateKey(rand.Reader);if err!=nil{t.Fatal(err)}
 now:=time.Now().UTC()
 data:=[]byte("%PDF-1.7\nsynthetic scan fixture; not a real incorporation certificate")
 sum:=sha256.Sum256(data)
 p:=ScanProof{EvidenceID:"evr_123456789",ArtifactID:"eart_123456789",MediaType:"application/pdf",
  Digest:"sha256:"+hex.EncodeToString(sum[:]),ScannerID:"synthetic-scanner",
  ScannerVersion:"v1",Outcome:"CLEAN",ScannedAt:now.Add(-time.Minute)}
 p.Signature=ed25519.Sign(private,p.SigningPayload())
 if err:=VerifyScanProof(p,public,now);err!=nil{t.Fatal(err)}
 wrong:=p;wrong.Outcome="INFECTED"
 if err:=VerifyScanProof(wrong,public,now);!errors.Is(err,ErrDocumentCustody){t.Fatalf("unsigned verdict accepted: %v",err)}
 wrong=p;wrong.ScannerVersion="v2"
 if err:=VerifyScanProof(wrong,public,now);err==nil{t.Fatal("scanner version substituted")}
 vault:=ScannedDocumentVault{Directory:t.TempDir(),Key:bytes.Repeat([]byte{0x45},32)}
 if err:=vault.Seal(context.Background(),p,data);err!=nil{t.Fatal(err)}
 if err:=vault.Seal(context.Background(),p,data);err!=nil{t.Fatalf("identical custody replay rejected: %v",err)}
 opened,err:=vault.Open(context.Background(),p)
 if err!=nil||!bytes.Equal(opened,data){t.Fatalf("sealed document lost integrity: %v",err)}
 wrong=p;wrong.ArtifactID="eart_another123"
 if _,err:=vault.Open(context.Background(),wrong);err==nil{t.Fatal("cross-artifact read allowed")}
 wrong=p;wrong.Digest="sha256:"+hex.EncodeToString(bytes.Repeat([]byte{0x00},32))
 if _,err:=vault.Open(context.Background(),wrong);err==nil{t.Fatal("document digest substituted")}
 if err:=vault.Seal(context.Background(),p,append(data,'X'));err==nil{t.Fatal("changed artifact overwrote custody")}
 entries,err:=os.ReadDir(filepath.Join(vault.Directory,".baobab-scanned-documents"))
 if err!=nil||len(entries)!=1{t.Fatalf("expected one sealed file: %v",err)}
 info,err:=entries[0].Info()
 if err!=nil||info.Mode().Perm()!=0600{t.Fatalf("unsafe custody mode: %v",err)}
}

func TestPEO03KCandidateEnvelopeBindsActorEvidenceAndDigest(t *testing.T) {
 now:=time.Now().UTC()
 attempt:=DocumentCandidateAttempt{CaseID:"vcase_synthetic1",ClaimID:"ecl_synthetic1",
  EvidenceID:"evr_synthetic1",ArtifactID:"eart_synthetic1",ActorID:"prn_extractor",
  Receipt:DocumentExtractionReceipt{
   EvidenceID:"evr_synthetic1",Standing:"DOCUMENT_CANDIDATE_ONLY",
   DocumentDigest:"sha256:"+hex.EncodeToString(bytes.Repeat([]byte{0x41},32)),
   ProcessorID:"certified-synthetic",ProcessorVersion:"v1",ObservedAt:now,
   Candidates:[]DocumentCandidate{{Field:"legal_name",Value:"Sensitive synthetic applicant",Confidence:0.93,Page:1}},
  }}
 key:=bytes.Repeat([]byte{0x83},32)
 ciphertext,err:=EncryptCandidates(key,attempt,"baobab-document-extractor-staging","synthetic-idempotency-value-01")
 if err!=nil{t.Fatal(err)}
 if bytes.Contains(ciphertext,[]byte("Sensitive synthetic applicant")){t.Fatal("plaintext stored in ciphertext")}
 found,err:=DecryptCandidates(key,ciphertext,attempt,"baobab-document-extractor-staging","synthetic-idempotency-value-01")
 if err!=nil||len(found)!=1||found[0].Value!=attempt.Receipt.Candidates[0].Value{t.Fatalf("cannot recover sealed candidate: %v",err)}
 changed:=attempt;changed.ActorID="prn_other"
 if _,err:=DecryptCandidates(key,ciphertext,changed,"baobab-document-extractor-staging","synthetic-idempotency-value-01");err==nil{t.Fatal("actor substitution allowed")}
 changed=attempt;changed.EvidenceID="evr_another"
 if _,err:=DecryptCandidates(key,ciphertext,changed,"baobab-document-extractor-staging","synthetic-idempotency-value-01");err==nil{t.Fatal("evidence substitution allowed")}
 if _,err:=DecryptCandidates(key,ciphertext,attempt,"verification:source-capture","synthetic-idempotency-value-01");err==nil{t.Fatal("client privilege substitution")}
 ciphertext[len(ciphertext)-1]^=1
 if _,err:=DecryptCandidates(key,ciphertext,attempt,"baobab-document-extractor-staging","synthetic-idempotency-value-01");err==nil{t.Fatal("tampered ciphertext accepted")}
 changed=attempt;changed.Receipt.Standing="VERIFIED"
 if _,err:=EncryptCandidates(key,changed,"baobab-document-extractor-staging","synthetic-idempotency-value-01");err==nil{t.Fatal("positive authority outcome accepted")}
}
