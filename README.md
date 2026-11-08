# permrag

A permission-aware RAG system for messy internal HR/ops documents: role and
department access control enforced **inside the SQL query itself**, so a
document an employee isn't authorized to see is never retrieved — not
filtered out afterward, never retrieved in the first place — no matter how
well it matches their query. Built for a non-technical HR/ops manager who
needs employees to get real answers from real internal docs without ever
risking a compensation band or a reorg plan leaking to the wrong person.

```
documents (+ access policy) ──▶ chunk (inherits policy) ──▶ SQLite FTS5 index
                                                                    │
query + (role, department) ──▶ SQL WHERE clause enforces ACL ──▶ permitted chunks only
                                                                    │
                                                          synthesize an answer
                                                                    │
                                                    audit log (itself access-gated)
```

## Why this project

The 2026 FDE hiring research named messy real-world integration pain and
documented tradeoffs as the actual differentiator — and for anything
touching internal company documents, the single most consequential
tradeoff is where access control lives. This project's answer: in the data
access layer, enforced by the database itself, not as application logic
anyone could get wrong later. See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)
for the full reasoning, including an FTS5 ranking gotcha (`bm25()` returns
*more negative* for a better match) verified empirically before being
relied on, and a real test-writing mistake caught and fixed along the way.

## Quickstart

```bash
make build
make test        # 47+ tests across 7 packages, no external services
make demo        # ingest example HR/ops docs, query as 6 different users, review the audit log
```

```bash
./bin/permrag ingest -dir testdata/docs
./bin/permrag ask -name eve -role employee -department sales -query "sales commission rate"
./bin/permrag audit -name hr-bob -role hr_admin   # review who asked what (HR/exec only)
```

## What's actually enforced, concretely

- **Role hierarchy**: employee < manager < hr_admin < exec. A document can
  require "manager and above" without enumerating every role.
- **Department restriction**: a document can be limited to specific
  departments — except hr_admin and exec, modeled as legitimately
  cross-departmental roles, who still have to satisfy any role-rank
  requirement on top of that.
- **Adversarial-tested**: a query using a restricted document's *exact*
  wording (which would rank extremely well) still returns zero results for
  an unauthorized user — the filter runs before ranking, not after.
- **No side-channel leak via the "N more documents exist" feature**: a
  denied query reports a suppressed count so the system doesn't look
  broken, but never a title or any content — tested explicitly, since even
  a restricted document's *title* can be sensitive on its own (`Q4 Layoffs
  Plan`).
- **The audit log is access-controlled the same way**: only hr_admin/exec
  can review who asked what; everyone else gets nothing, not an error that
  a caller could mishandle.

## Project layout

```
internal/acl/        role hierarchy + department policy model
internal/docstore/    documents, chunking, chunk-level ACL inheritance
internal/search/       SQLite FTS5 index with ACL enforced in SQL
internal/synth/         answer synthesis (Fake for tests/demo, HTTP for real LLMs)
internal/audit/         access-gated query log
internal/ingest/        manifest-driven document loading
internal/engine/        orchestrates search + synth + audit into one Ask() call
cmd/permrag/             CLI: ingest, ask, audit
testdata/docs/           example HR/ops documents with varied ACLs
docs/ARCHITECTURE.md     design rationale
```

## What's deliberately out of scope (v1)

See docs/ARCHITECTURE.md — no embedding-based semantic retrieval (the
ACL-enforcement problem this project solves is identical in shape for
lexical or semantic retrieval), no per-chunk ACL overrides within a single
document, and no real LLM call in the default demo path (`synth.HTTPSynth`
is there for real production use against any OpenAI-compatible endpoint).
