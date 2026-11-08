// Command permrag is the CLI: `ingest` indexes a manifest directory of
// documents, `ask` runs a permission-aware query as a given user, and
// `audit` reviews the query log (itself access-gated — see
// internal/audit).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/Kuwar-Vishnudatt/permrag/internal/acl"
	"github.com/Kuwar-Vishnudatt/permrag/internal/audit"
	"github.com/Kuwar-Vishnudatt/permrag/internal/docstore"
	"github.com/Kuwar-Vishnudatt/permrag/internal/engine"
	"github.com/Kuwar-Vishnudatt/permrag/internal/ingest"
	"github.com/Kuwar-Vishnudatt/permrag/internal/search"
	"github.com/Kuwar-Vishnudatt/permrag/internal/synth"
)

const (
	defaultSearchDB = "permrag-search.db"
	defaultAuditDB  = "permrag-audit.db"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "ingest":
		runIngest(os.Args[2:])
	case "ask":
		runAsk(os.Args[2:])
	case "audit":
		runAudit(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: permrag <ingest|ask|audit> [flags]")
}

func runIngest(args []string) {
	fs := flag.NewFlagSet("ingest", flag.ExitOnError)
	dir := fs.String("dir", "", "directory containing manifest.yaml and document bodies (required)")
	searchDB := fs.String("search-db", defaultSearchDB, "path to the search index database")
	fs.Parse(args)

	if *dir == "" {
		fmt.Fprintln(os.Stderr, "ingest: -dir is required")
		os.Exit(2)
	}

	docs, err := ingest.LoadManifest(*dir)
	must(err)

	ix, err := search.Open(*searchDB)
	must(err)
	defer ix.Close()

	chunkCount := 0
	for _, d := range docs {
		for _, c := range docstore.ChunkDocument(d) {
			must(ix.Add(c))
			chunkCount++
		}
	}
	fmt.Printf("indexed %d documents (%d chunks) into %s\n", len(docs), chunkCount, *searchDB)
}

func runAsk(args []string) {
	fs := flag.NewFlagSet("ask", flag.ExitOnError)
	searchDB := fs.String("search-db", defaultSearchDB, "path to the search index database")
	auditDB := fs.String("audit-db", defaultAuditDB, "path to the audit log database")
	name := fs.String("name", "", "the requesting user's name (required)")
	role := fs.String("role", "", "employee|manager|hr_admin|exec (required)")
	department := fs.String("department", "", "the requesting user's department")
	query := fs.String("query", "", "the question to ask (required)")
	fs.Parse(args)

	if *name == "" || *role == "" || *query == "" {
		fmt.Fprintln(os.Stderr, "ask: -name, -role, and -query are required")
		os.Exit(2)
	}
	if acl.Rank(acl.Role(*role)) < 0 {
		fmt.Fprintf(os.Stderr, "ask: unknown role %q\n", *role)
		os.Exit(2)
	}

	ix, err := search.Open(*searchDB)
	must(err)
	defer ix.Close()
	al, err := audit.Open(*auditDB)
	must(err)
	defer al.Close()

	e := engine.New(ix, synth.Fake{}, al)
	u := acl.User{Name: *name, Role: acl.Role(*role), Department: *department}

	res, err := e.Ask(context.Background(), u, *query)
	must(err)

	fmt.Println(res.Answer.Text)
	if len(res.Answer.Sources) > 0 {
		fmt.Println("\nSources:")
		for _, s := range res.Answer.Sources {
			fmt.Printf("  - %s\n", s.DocTitle)
		}
	}
	if res.SuppressedCount > 0 {
		fmt.Printf("\n(%d additional relevant document(s) exist but require higher access to view)\n", res.SuppressedCount)
	}
}

func runAudit(args []string) {
	fs := flag.NewFlagSet("audit", flag.ExitOnError)
	auditDB := fs.String("audit-db", defaultAuditDB, "path to the audit log database")
	name := fs.String("name", "", "the requesting reviewer's name (required)")
	role := fs.String("role", "", "must be hr_admin or exec to review the log (required)")
	fs.Parse(args)

	if *name == "" || *role == "" {
		fmt.Fprintln(os.Stderr, "audit: -name and -role are required")
		os.Exit(2)
	}

	al, err := audit.Open(*auditDB)
	must(err)
	defer al.Close()

	entries, err := al.Review(acl.User{Name: *name, Role: acl.Role(*role)})
	must(err)

	if len(entries) == 0 {
		fmt.Println("(no entries visible — either the log is empty, or your role is not permitted to review it)")
		return
	}
	for _, e := range entries {
		fmt.Printf("%s | %-10s %-20s dept=%-12s query=%q permitted=%d suppressed=%d\n",
			e.Time.Format("2006-01-02 15:04:05"), e.UserRole, e.UserName, e.UserDepartment, e.Query, e.PermittedCount, e.SuppressedCount)
	}
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
