// document-custody is a separately provisioned custody maintenance workload.
// Its ordinary invocation reports metadata; each mutation requires a private
// two-person approved snapshot job. It never receives scanner signing keys.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/scannerruntime"
)

func main() {
	config := flag.String("custody-config", "", "absolute private approved custody configuration")
	approval := flag.String("approved-job", "", "absolute private approved snapshot job; omitted reports only")
	afterEvidence := flag.String("after-evidence", "", "report pagination cursor evidence")
	afterArtifact := flag.String("after-artifact", "", "report pagination cursor artifact")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	if os.Getenv("BAOBAB_ENVIRONMENT") != "staging" && os.Getenv("BAOBAB_ENVIRONMENT") != "integration" {
		denied()
	}
	repo, err := repository.Open(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		denied()
	}
	defer repo.Close()
	receipts, err := scannerruntime.RunCustodyMaintenance(ctx, *config, *approval, *afterEvidence, *afterArtifact, repo, domain.NewUUIDv7())
	if err != nil {
		denied()
	}
	if json.NewEncoder(os.Stdout).Encode(receipts) != nil {
		os.Exit(1)
	}
}
func denied() {
	fmt.Fprintln(os.Stderr, "custody maintenance denied or unavailable; review the approved snapshot and protected operational evidence")
	os.Exit(1)
}
