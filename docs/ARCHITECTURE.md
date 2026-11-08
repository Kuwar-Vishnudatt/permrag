# Architecture

## What problem this solves

The 2026 FDE hiring research was specific about what separates a shippable
forward-deployed tool from a demo: handling messy real-world integration
pain, documenting tradeoffs, and — for anything touching internal
documents — getting access control right, because an HR/ops RAG system
that occasionally answers an employee's question using a confidential
compensation document isn't a bug to fix later, it's the kind of incident
that ends the project. This system is built around one non-negotiable
design constraint: a restricted document must be **structurally
impossible** to retrieve for a user who isn't allowed to see it, not merely
filtered out by application logic that a future change could get wrong.

## The core design decision: enforce ACLs inside the SQL WHERE clause, not after

The tempting-but-wrong architecture is: run the search query, get back
every matching chunk, THEN filter out what the user isn't allowed to see,
THEN pass the rest to the LLM. That "then filter" step is exactly the kind
of thing a future refactor, a new code path, or a rushed feature addition
forgets to do correctly — the access check becomes something a caller has
to remember, rather than something the data access layer itself guarantees.

`internal/search.Index.Search` instead puts the ACL check directly in the
SQL `WHERE` clause (`internal/search/search.go`): a restricted chunk simply
never becomes a result **row** for an unauthorized user. There is no later
point in the pipeline — `synth.Synthesizer`, the audit log, the CLI output
— that ever sees it in the first place, so there's no code path that could
accidentally leak it by failing to filter correctly, because filtering
isn't a separate step at all.

This is proven, not just asserted: `TestAdversarialExactKeywordQueryStillRespectsACL`
constructs a query using a restricted document's *exact* wording (which
would rank extremely well) and confirms it still returns zero results for
an unauthorized role — demonstrating the filter applies **before** ranking
takes effect, not as a post-hoc check that a sufficiently well-matched
restricted result might slip past.

## The ACL model: role rank + department, with an explicit cross-departmental override

`internal/acl` models two independent restrictions a document can carry:

- **MinRole**: a role hierarchy (employee < manager < hr_admin < exec), so
  a policy can say "manager and above" instead of enumerating every role
  that should see it — the way HR/ops policies are actually written, and
  one that doesn't need updating every time a new role is added.
- **Departments**: an explicit allow-list. A document with no department
  restriction is visible to any department; one with a restriction is
  visible only to the listed departments — **except** hr_admin and exec,
  which are modeled as cross-departmental roles (an HR admin legitimately
  needs to see engineering's on-call runbook, a sales team's commission
  structure, and everything else, as part of the job). This is a real
  design decision, not an oversight: `TestHRAdminStillSubjectToMinRole`
  confirms the override only waives the *department* check — an hr_admin
  still has to satisfy an exec-only MinRole like anyone else.

A document with **no policy at all** is public — explicit, not an
accidental fail-open: declaring a restriction is something a document
author does on purpose (see `internal/ingest`'s manifest format), so
there's never a state where "I forgot to set a policy" silently becomes
"now it's restricted" or, worse, the reverse.

## Chunk-level ACL inheritance

A chunk is a standalone retrievable unit, indexed independently of its
parent document. `internal/docstore.ChunkDocument` copies the parent
document's `Policy` onto every chunk it produces — not a reference, a
value copy — specifically so nothing downstream ever needs to trust a join
back to a documents table to know a chunk's access requirement.
`internal/search.Index.Add` then stores that policy as queryable columns
(`min_role_rank` as an integer, and departments in a proper many-to-many
join table rather than a delimited string) precisely so the SQL `WHERE`
clause can compare against it directly, with no string-matching logic
(a `LIKE`-based match on a comma-joined department column would be exactly
the kind of thing that could wrongly match "eng" against
"engineering-finance" — an exact-match join table has no such failure
mode).

## The "how much was withheld" feature, and why it can only ever be a count

A genuinely useful UX signal for a non-technical HR/ops user: "3 more
relevant documents exist but require manager-level access" — so a denied
query doesn't look like the system found nothing, it looks like the system
correctly enforced a boundary. The risk is that computing this naively
(re-running the search without the ACL filter and returning *something*
about those extra results) becomes its own leak: a document's **title
alone** can be sensitive (`Q4 Layoffs Plan` tells you something is
happening before the announcement, even with zero body text attached).

`internal/search.Index.countSuppressed` re-runs the match query with no
ACL filter, but reads back **only `chunk_id`** — never `doc_title`, never
`body` — and returns nothing but an integer count.
`TestSuppressedCountNeverLeaksTitleOrContent` asserts this structurally: for
a query that definitely matches a restricted document, the visible
`SearchResult.Results` is empty and `SuppressedCount` is 1 — there is no
field anywhere in the returned struct that could carry the restricted
document's identity even by a future programmer's mistake, because the
type itself doesn't have one.

## The audit log is itself access-controlled, the same way

`internal/audit.Log.Review` applies the identical discipline as search: an
unauthorized caller (anyone other than hr_admin/exec) gets an **empty
slice and no error**, not a boolean a caller has to check before deciding
whether to display results. `TestReviewDeniedToManager` specifically
checks that a manager — higher-ranked than an employee, but not
cross-departmental — is still denied, proving the gate is role-specific,
not just "high enough rank."

## Why FTS5, and the bm25() ordering gotcha it was deliberately built around

`modernc.org/sqlite` (the pure-Go SQLite driver already used elsewhere in
this portfolio) ships with FTS5 compiled in, giving real lexical search
with BM25 ranking rather than a hand-rolled TF-IDF implementation — genuine
infrastructure, consistent with this portfolio's preference for real tools
over toy reimplementations where a lightweight real option exists.

One thing worth calling out explicitly because it's a well-known footgun:
FTS5's `bm25()` function returns a score where **more negative is a better
match** — the opposite of the "higher score wins" convention most people
assume. `ORDER BY bm25(table) DESC` would silently return the *worst*
matches first while looking completely normal (same rows, same columns,
just backwards). This was verified directly against a real FTS5 table
before writing any application code (three documents with "cat" at
different relevance densities, confirming the sign empirically rather than
trusting a half-remembered detail), and `internal/search` orders `ASC`
accordingly. `TestResultsOrderedBestMatchFirst` exists specifically to
catch a future regression of this exact mistake.

## Testing strategy

- `internal/acl`: every combination of role-rank and department gating,
  including the cross-departmental override and that it doesn't also waive
  MinRole, and a fail-closed check for an unrecognized role.
- `internal/docstore`: chunk-level policy inheritance and chunk ID
  stability/uniqueness.
- `internal/search`: the security-critical suite — role and department
  restriction enforcement, the HR/exec override, combined role+department
  gating, the adversarial exact-keyword test, the suppressed-count
  non-leak test, that *every* chunk of a multi-chunk restricted document is
  covered (not just the first), and the bm25 ordering regression test.
- `internal/synth`: the Fake synthesizer only ever answers from what it was
  handed, with no path to reach outside that slice.
- `internal/audit`: record/review round-trip and the review-gate tests
  (denied to employee and manager, allowed to hr_admin and exec).
- `internal/engine`: full-pipeline tests tying search + synth + audit
  together, including a leak-check against a phrase that exists ONLY in
  the restricted document's body (not in the query text) — an earlier
  draft of this same test checked for a word that also appeared in the
  query itself, which would have passed even with a broken filter, since
  the Fake's "no results" message echoes the query back; this was caught
  immediately when the (correct) code "failed" the (flawed) assertion,
  and fixed by choosing an unambiguous phrase instead.

## What's deliberately out of scope (v1)

- **A real embedding-based semantic retriever.** Lexical (FTS5/BM25)
  retrieval is the right scope here: the engineering problem this project
  solves is ACL enforcement at the retrieval layer, which is identical in
  shape whether the underlying match is lexical or semantic — swapping in
  an embedding index later wouldn't change any of the access-control
  logic this project actually tests.
- **Per-chunk (rather than per-document) ACL overrides.** Every chunk of a
  document shares its parent's policy; a document that needs e.g. one
  paragraph redacted for a lower role would need to be split into separate
  documents under this model. A reasonable v2 extension, not attempted
  here to keep the policy model easy to reason about end to end.
- **A real LLM call in the default demo path.** `synth.Fake` is what every
  test and the bundled demo use — deterministic, free, and exercising
  exactly the same `[]search.Result -> Answer` boundary a real call would.
  `synth.HTTPSynth` wires to any OpenAI-compatible endpoint for actual
  production use.
