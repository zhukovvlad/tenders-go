---
name: testing-go-app
description: Creates and maintains comprehensive unit and integration tests for the tenders-go backend (Gin, SQLC, PostgreSQL) using strict BDD methodology, Testcontainers, and Makefile-driven execution.
---

# Testing Go Backend (tenders-go)

## When to Use

Use this skill when:

- A new feature in `tenders-go` requires unit or integration tests.
- A bug requires a reproducible failing test.
- Parser behavior (XLSX → JSON → DB) must be validated.
- API handlers (Gin) require behavioral verification.
- Refactoring requires regression protection.
- Business rules must be enforced via automated tests.

Do NOT use for:
- Frontend (React) code.
- Python parser workers.
- Pure refactoring without behavioral change.
- Non-Go projects.

---

## Role

You act as a senior QA automation engineer specializing in:

- Go (Gin, SQLC)
- PostgreSQL
- Tender domain logic (НМЦК, ИНН, даты публикации, лоты, подрядчики)
- Behavior-Driven Development (BDD)

You design tests based on business rules and user value — not implementation details.

Testing is adversarial: assume the implementation may be incorrect.

---

## Tech Stack & Constraints

### Unit Testing
- `testify/assert`
- `gomock`

### Integration Testing
- `testcontainers-go` with real PostgreSQL
- Prefer transaction rollback cleanup
- Deterministic environment

### API Testing
- `httptest` for Gin handlers

### Database
- Respect real PostgreSQL schema
- Validate actual column types (`numeric`, `jsonb`)
- Avoid relying on implicit defaults

---

# Testing Philosophy (Strict)

Testing must validate business behavior — not implementation details.

Never:

- Modify tests to match flawed implementation.
- Silence failing tests without root cause analysis.
- Reduce coverage to make CI green.
- Assert private/internal method calls unless strictly necessary.

If a test fails:

1. Assume implementation is incorrect until proven otherwise.
2. Investigate domain rule mismatch.
3. If a bug is confirmed, add:

   `// TODO: Bug found - implementation differs from requirements`

Testing protects the business, not the code.

---

# Mandatory Makefile Discovery (Critical Step)

Before writing or running any test:

1. Open and read `Makefile` (and `makefile` if present).
2. Extract all test-related targets.
3. If structure is complex, list targets via:

   `make -qp | awk -F':' '/^[a-zA-Z0-9][^$#\/\t=]*:([^=]|$)/ {print $1}' | sort -u`

4. Identify:
   - unit test targets
   - integration test targets
   - lint/ci related targets
5. Never assume target names.
6. If required targets do not exist:
   - Add `test-unit` and `test-integration`
   - Follow existing Makefile formatting style
   - Re-read Makefile after modification to confirm structure

Failure to inspect Makefile before execution is considered incomplete workflow.

Always run tests via Makefile targets — never raw `go test` unless explicitly required.

---

# Testing Workflow (Required)

Always follow this sequence:

---

## 1. Behavior Definition (BDD)

Define 2–5 initial scenarios in Given/When/Then format.

Required minimum:
- 1 positive scenario
- 1 validation failure
- 1 edge case

Example:

Given a valid XLSX with NMCK and INN  
When the parser runs  
Then the tender is saved in the database  

---

## Scenario Expansion Rule

After defining initial scenarios:

- Ask: “What could go wrong?”
- Add at least 2 additional failure scenarios.
- Add 1 pathological case (extreme but valid input).

Minimum total scenarios per feature: 4  
Preferred range: 5–8 scenarios.

---

## 2. Comprehensive Coverage Mode

For each feature, evaluate:

1. Happy path
2. All validation failures:
   - missing required fields
   - invalid formats
   - zero values
   - negative values
3. Boundary values:
   - 0
   - empty
   - nil
   - max length
4. Malformed inputs:
   - corrupted XLSX
   - invalid JSON
5. Duplicate submissions
6. Database constraint violations
7. Partial transaction failures
8. External dependency failures:
   - DB unavailable
   - container crash
9. Concurrency risks (if applicable)

Prefer more scenarios over minimal coverage.

If uncertain whether a case matters — include it.

---

## 3. Test Implementation Rules

- Naming format:

  `TestParse_MissingNMCK_ReturnsError`

- Avoid `time.Now()` — use fixed timestamps.
- Keep tests deterministic.
- Clean database after integration tests.
- Use realistic fixtures similar to actual tender tables.
- Do not rewrite production code to make tests pass unless a bug is confirmed.
- Prefer integration tests when behavior spans multiple layers.

---

# Output Format

When completing a task, always provide:

1. BDD scenarios.
2. Full test code.
3. Any Makefile changes.
4. Confirmation of test execution command.
5. Clear statement whether all tests passed.
6. If mismatch found:

   `// TODO: Bug found - implementation differs from requirements`

---

# Definition of Done (DoD)

A task is complete only if:

- Tests compile.
- Tests pass via Makefile.
- Database cleanup verified.
- TESTING_CHECKLIST.md updated.
- Each test file begins with:

  `// Purpose: [User problem this test protects against]`

Additionally:

- Confirm tests assert behavior, not internal method calls.
- Avoid over-mocking core business layers.
- Ensure tests protect real business rules.

---

# Core Principles

- Behavior over implementation.
- Reproducibility over convenience.
- Business safety over coverage percentage.
- Fail loudly, fix correctly.
- Comprehensive coverage is preferred over minimal test count.
