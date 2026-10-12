// independent-scanner is run in a separately isolated staging workload. Never
// mount its signing key into the Control Plane or document-extractor process.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/scannerruntime"
)

func main() {
	config := flag.String("operator-config", "", "absolute private scanner job configuration")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	receipt, err := scannerruntime.RunScanner(ctx, *config, &http.Client{Timeout: 30 * time.Second})
	if err != nil {
		fmt.Fprintln(os.Stderr, "independent scan submission denied or unavailable; retain signed job for reviewed replay")
		os.Exit(1)
	}
	// Only bounded redacted protocol receipt enters stdout. All errors remain
	// opaque so URLs, documents, IAM credentials and signer bytes cannot leak.
	if json.NewEncoder(os.Stdout).Encode(receipt) != nil {
		os.Exit(1)
	}
}
