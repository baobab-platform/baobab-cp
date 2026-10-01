// Command admin-population checks a reviewed administrator population
// (Shared administration/v1 ReviewedPopulation) before anyone turns it into
// AdministrativeGrants (roles-to-grants decision, rulings 3 and 5).
//
// The reviewed list is owned by Platform Security / Control Plane Governance.
// This command only reads a file: it connects to nothing, writes nothing and
// issues no grant. It reports what is wrong with the list, and, for an
// APPROVED list with nothing wrong, the requests an authorised administrator
// then makes of the grant administration API (HIGH and CRITICAL ones as
// ADMINISTRATIVE_GRANT_ISSUANCE changesets with an independent approver).
//
//	admin-population -file population.json
//
// Exit status: 0 the population is valid, 1 it has findings, 2 the file
// could not be read.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/administration"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, time.Now)) }

func run(args []string, stdout, stderr io.Writer, now func() time.Time) int {
	fs := flag.NewFlagSet("admin-population", flag.ContinueOnError)
	fs.SetOutput(stderr)
	file := fs.String("file", "", "path of the ReviewedPopulation JSON document")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *file == "" {
		fmt.Fprintln(stderr, "-file is required")
		return 2
	}
	raw, err := os.ReadFile(*file)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	population, err := administration.ParsePopulation(raw)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	report := administration.CheckPopulation(administration.MustDefaultCatalogue(), administration.MustDefaultSoD(), population, now())
	out, _ := json.MarshalIndent(report, "", "  ")
	fmt.Fprintln(stdout, string(out))
	if !report.Valid {
		return 1
	}
	return 0
}
