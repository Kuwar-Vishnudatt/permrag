#!/usr/bin/env bash
# Ingests the bundled example HR/ops documents, then runs a sequence of
# queries as different roles/departments to show the ACL boundaries live:
# same-department access, cross-department denial, role-rank gating, the
# HR/exec cross-departmental override, an adversarial exact-keyword
# attempt against a restricted document, and audit log review (itself
# access-gated).
set -euo pipefail
cd "$(dirname "$0")/.."

SEARCH_DB=$(mktemp -t permrag-search.XXXXXX.db)
AUDIT_DB=$(mktemp -t permrag-audit.XXXXXX.db)
rm -f "$SEARCH_DB" "$AUDIT_DB"
trap 'rm -f "$SEARCH_DB" "$AUDIT_DB"' EXIT

ask() {
  echo "--- $1 ---"
  shift
  ./bin/permrag ask -search-db "$SEARCH_DB" -audit-db "$AUDIT_DB" "$@"
  echo
}

echo "=== ingesting example HR/ops documents ==="
./bin/permrag ingest -dir testdata/docs -search-db "$SEARCH_DB"
echo

ask "employee (sales) asks about executive compensation: DENIED" \
  -name eve -role employee -department sales -query "executive compensation bands equity grants"

ask "employee (sales) asks about sales commission: own department, ALLOWED" \
  -name eve -role employee -department sales -query "sales commission accelerator rate"

ask "employee (sales) asks about engineering on-call: wrong department, DENIED" \
  -name eve -role employee -department sales -query "on-call escalation engineering"

ask "exec asks about executive compensation: ALLOWED" \
  -name ceo -role exec -department exec -query "executive compensation bands equity grants"

ask "hr_admin (finance) asks about sales commission: cross-department override, ALLOWED" \
  -name hr-bob -role hr_admin -department finance -query "sales commission accelerator rate"

ask "employee attempts an adversarial exact-keyword query against the layoffs plan: DENIED" \
  -name eve -role employee -department sales -query "reduction in force severance reorganization confidential"

echo "--- employee attempts to review the audit log: DENIED ---"
./bin/permrag audit -audit-db "$AUDIT_DB" -name eve -role employee
echo

echo "--- hr_admin reviews the audit log: every query above, with suppressed counts ---"
./bin/permrag audit -audit-db "$AUDIT_DB" -name hr-bob -role hr_admin
