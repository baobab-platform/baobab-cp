package authority

import (
 "context"
 "crypto/aes"
 "crypto/cipher"
 "crypto/ed25519"
 "crypto/rand"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "io"
 "os"
 "path/filepath"
 "strings"
 "time"
)

var ErrDocumentCustody = errors.New("document custody or scan signature invalid")
const MaxScannedDocumentBytes = 8 * 1024 * 1024

// ScanProof is an independent scanner's signed claim about exact immutable
// bytes, not a claim that a legal entity is valid. The scanner's private key
// must NEVER be provisioned to the Control Plane.
type ScanProof struct {
 EvidenceID string
 ArtifactID string
 MediaType string
 Digest string
 ScannerID string
 ScannerVersion string
 Outcome string
 ScannedAt time.Time
 Signature []byte
}

// SigningPayload provides the canonical bytes for the independent scanner.\nfunc (p ScanProof) SigningPayload() []byte {
 v := struct{
  EvidenceID string `json:"evidence_id"`
  ArtifactID string `json:"artifact_id"`
  MediaType string `json:"media_type"`
  Digest string `json:"digest"`
  ScannerID string `json:"scanner_id"`
  ScannerVersion string `json:"scanner_version"`
  Outcome string `json:"outcome"`
  ScannedAt string `json:"scanned_at"`
 }{p.EvidenceID,p.ArtifactID,p.MediaType,p.Digest,p.ScannerID,p.ScannerVersion,p.Outcome,p.ScannedAt.UTC().Format(time.RFC3339Nano)}
 raw,_:=json.Marshal(v)
 return raw
}

// VerifyScanProof only trusts an out-of-band configured scanner public key
// paired with the exact scanner_id/version. No caller can self-assert CLEAN.
func VerifyScanProof(p ScanProof, publicKey ed25519.PublicKey, now time.Time) error {
 if len(publicKey)!=ed25519.PublicKeySize || len(p.Signature)!=ed25519.SignatureSize ||
  !strings.HasPrefix(p.EvidenceID,"evr_") || len(p.EvidenceID)<8 ||
  !strings.HasPrefix(p.ArtifactID,"eart_") || len(p.ArtifactID)<8 ||
  p.ScannerID=="" || p.ScannerVersion=="" ||
  p.Outcome!="CLEAN" || p.ScannedAt.IsZero() ||
  p.ScannedAt.After(now.UTC()) || now.Sub(p.ScannedAt)>365*24*time.Hour ||
  len(p.Digest)!=71 || !strings.HasPrefix(p.Digest,"sha256:") {
  return ErrDocumentCustody
 }
 if _,err:=hex.DecodeString(strings.TrimPrefix(p.Digest,"sha256:"));err!=nil{return ErrDocumentCustody}
 switch p.MediaType {
 case "application/pdf","image/jpeg","image/png":
 default:return ErrDocumentCustody
 }
 if !ed25519.Verify(publicKey,p.SigningPayload(),p.Signature) {return ErrDocumentCustody}
 return nil
}

// ScannedDocumentVault is a local mounted-key encrypted artifact adapter for
// tests and protected staging. Production S3/KMS requires its own approved
// backend and object lifecycle controls, not shared filesystem permissions.
type ScannedDocumentVault struct {
 Directory string
 Key []byte
}

func (v ScannedDocumentVault) location(evidenceID,artifactID string)(string,error) {
 if !safeCustodyDirectory(v.Directory) || len(v.Key)!=32 ||
  !strings.HasPrefix(evidenceID,"evr_") || !strings.HasPrefix(artifactID,"eart_") ||
  len(evidenceID)<8 || len(artifactID)<8 || len(evidenceID)>96 || len(artifactID)>96 {
  return "",ErrDocumentCustody
 }
 sum:=sha256.Sum256([]byte(evidenceID+"\n"+artifactID))
 return filepath.Join(v.Directory,".baobab-scanned-documents",hex.EncodeToString(sum[:])+".sealed"),nil
}

func (v ScannedDocumentVault) Seal(ctx context.Context, p ScanProof, bytes []byte)(error) {
 if ctx==nil || ctx.Err()!=nil || len(bytes)<8 || len(bytes)>MaxScannedDocumentBytes ||
  !validDocumentSignature(p.MediaType,bytes) {return ErrDocumentCustody}
 sum:=sha256.Sum256(bytes)
 if p.Digest!="sha256:"+hex.EncodeToString(sum[:]) {return ErrDocumentCustody}
 path,err:=v.location(p.EvidenceID,p.ArtifactID)
 if err!=nil{return err}
 block,err:=aes.NewCipher(v.Key);if err!=nil{return ErrDocumentCustody}
 aead,err:=cipher.NewGCM(block);if err!=nil{return ErrDocumentCustody}
 nonce:=make([]byte,aead.NonceSize())
 if _,err=rand.Read(nonce);err!=nil{return ErrDocumentCustody}
 aad:=[]byte(p.EvidenceID+"\n"+p.ArtifactID+"\n"+p.Digest)
 sealed:=aead.Seal(nonce,nonce,bytes,aad)
 if err=os.MkdirAll(v.Directory,0700);err!=nil{return ErrDocumentCustody}
 parent,err:=os.Lstat(v.Directory);if err!=nil||!parent.IsDir()||parent.Mode()&os.ModeSymlink!=0 {return ErrDocumentCustody}
 dir:=filepath.Dir(path)
 if err=os.Mkdir(dir,0700);err!=nil&&!errors.Is(err,os.ErrExist){return ErrDocumentCustody}
 info,err:=os.Lstat(dir)
 if err!=nil||!info.IsDir()||info.Mode()&os.ModeSymlink!=0||info.Mode().Perm()&0077!=0{return ErrDocumentCustody}
 file,err:=os.OpenFile(path,os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600)
 if errors.Is(err,os.ErrExist) {
  existing,openErr:=v.Open(ctx,p)
  if openErr!=nil||!equalDocument(existing,bytes){return ErrDocumentCustody}
  return nil
 }
 if err!=nil{return ErrDocumentCustody}
 _,err=file.Write(sealed)
 if err==nil {err=file.Sync()}
 closeErr:=file.Close()
 if err!=nil||closeErr!=nil {_=os.Remove(path);return ErrDocumentCustody}
 return nil
}

func equalDocument(a,b []byte)bool {
 if len(a)!=len(b){return false}
 ha:=sha256.Sum256(a);hb:=sha256.Sum256(b)
 return ha==hb
}

func (v ScannedDocumentVault) Open(ctx context.Context,p ScanProof)([]byte,error) {
 if ctx==nil||ctx.Err()!=nil{return nil,ErrDocumentCustody}
 path,err:=v.location(p.EvidenceID,p.ArtifactID)
 if err!=nil{return nil,err}
 info,err:=os.Lstat(filepath.Dir(path))
 if err!=nil||!info.IsDir()||info.Mode()&os.ModeSymlink!=0||info.Mode().Perm()&0077!=0{return nil,ErrDocumentCustody}
 f,err:=os.Open(path);if err!=nil{return nil,ErrDocumentCustody}
 defer f.Close()
 st,err:=f.Stat()
 if err!=nil||!st.Mode().IsRegular()||st.Mode().Perm()&0077!=0||st.Size()>MaxScannedDocumentBytes+128{return nil,ErrDocumentCustody}
 raw,err:=io.ReadAll(io.LimitReader(f,MaxScannedDocumentBytes+129))
 if err!=nil||len(raw)>MaxScannedDocumentBytes+128{return nil,ErrDocumentCustody}
 block,err:=aes.NewCipher(v.Key);if err!=nil{return nil,ErrDocumentCustody}
 aead,err:=cipher.NewGCM(block)
 if err!=nil||len(raw)<aead.NonceSize()+aead.Overhead(){return nil,ErrDocumentCustody}
 aad:=[]byte(p.EvidenceID+"\n"+p.ArtifactID+"\n"+p.Digest)
 clear,err:=aead.Open(nil,raw[:aead.NonceSize()],raw[aead.NonceSize():],aad)
 if err!=nil||len(clear)>MaxScannedDocumentBytes||!validDocumentSignature(p.MediaType,clear){return nil,ErrDocumentCustody}
 sum:=sha256.Sum256(clear)
 if p.Digest!="sha256:"+hex.EncodeToString(sum[:]) {return nil,ErrDocumentCustody}
 return clear,nil
}
