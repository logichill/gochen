#!/usr/bin/env bash
# Validate each sibling module independently, then run the real service suites.
set -euo pipefail

if [[ "${1:-}" == "--help" ]]; then
  echo "Usage: bash scripts/validate-workspace.sh [log-directory]"
  echo "Runs independent build/vet/tests for all nine modules, Runtime examples,"
  echo "ALife unit/race/coverage/benchmarks, and ALife/ERP/EMS API suites."
  exit 0
fi

workspace_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
validation_logs="${1:-$(mktemp -d /tmp/gochen-workspace-validation.XXXXXX)}"
mkdir -p "${validation_logs}"
validation_logs="$(cd "${validation_logs}" && pwd)"
export GOWORK=off
export GOMAXPROCS="${GOMAXPROCS:-2}"
export GOFLAGS="${GOFLAGS:--p=2}"
validation_failures=0
validation_steps=0

run_step() {
  local project="$1" label="$2"
  shift 2
  local log="${validation_logs}/${project}-${label}.log"
  validation_steps=$((validation_steps + 1))
  echo "Running ${project}: ${label}"
  if (cd "${workspace_root}/${project}" && "$@") >"${log}" 2>&1; then
    echo "PASS ${project}: ${label}"
  else
    local result=$?
    echo "FAIL ${project}: ${label} (exit ${result}); ${log}" >&2
    tail -n 30 "${log}" >&2
    validation_failures=$((validation_failures + 1))
  fi
}

for project in gochen gochen-runtime gochen-contrib gochen-iam gochen-llm gochen-workflow alife erp ems; do
  run_step "${project}" build go build ./...
  run_step "${project}" vet go vet ./...
  run_step "${project}" test go test -count=1 ./...
done

# The zero-dependency property must be checked outside workspace resolution.
run_step gochen dependency-boundary bash -c '[[ "$(go list -m all)" == gochen ]] && [[ ! -f go.sum ]]'
run_step gochen-runtime examples make examples-smoke
run_step alife unit-script bash scripts/tests/unit.sh
run_step alife api-script bash scripts/tests/api.sh
run_step erp api-script bash scripts/tests/api.sh
run_step ems api-script bash scripts/tests/api/run_all.sh

echo "Validation finished: ${validation_steps} steps, ${validation_failures} failures. Logs: ${validation_logs}"
[[ "${validation_failures}" -eq 0 ]]
