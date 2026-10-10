package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/auth"
)

func TestCurrentInternalAuthorityRejectsUnregisteredWorkloadAndHuman(t *testing.T) {
	handler:=internalAuthorityHandler{clientID:"baobab-subscriptions"}
	path:="/internal/subscriptions/v1/tenants/tn_synthetic/product-subscriptions/sub_synthetic/internal-authority?classification_reference=adm_synthetic"
	for _,test:=range []struct {
		name string
		principal auth.Principal
	}{
		{name:"human",principal:auth.Principal{ActorType:"human",ClientID:"baobab-subscriptions"}},
		{name:"wrong workload",principal:auth.Principal{ActorType:"workload",ClientID:"other-engine"}},
		{name:"missing client",principal:auth.Principal{ActorType:"workload"}},
	} {
		t.Run(test.name,func(t *testing.T){
			r:=httptest.NewRequest(http.MethodGet,path,nil)
			r=r.WithContext(auth.WithPrincipal(r.Context(),test.principal))
			w:=httptest.NewRecorder()
			handler.read(w,r)
			if w.Code!=http.StatusForbidden {
				t.Fatalf("caller %q got %d, want 403",test.name,w.Code)
			}
			if w.Header().Get("Cache-Control")!="no-store, max-age=0" {
				t.Fatal("an unauthorised PDP response could be cached")
			}
		})
	}
}
