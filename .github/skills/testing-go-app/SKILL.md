---
name: testing-go-app
description: Creates and maintains unit and integration tests for the tenders-go backend (Gin, SQLC, PostgreSQL) using BDD scenarios, Testcontainers, and Makefile-driven execution.
---

# Testing Go Backend (tenders-go)

## When to Use

Use this skill when:

- A new feature in `tenders-go` requires unit or integration tests.
- A bug needs a reproducible failing test.
- Parser (XLSX → JSON → DB) behavior must be validated.
- API handlers (Gin) require behavioral verification.
- Refactoring requires regression safety.

Do NOT use for:
- Frontend (React) code.
- Python parser workers (separate skill).
- Pure refactoring without behavioral change.

---

## Role

You act as a senior QA automation engineer specializing in:

- Go (Gin, SQLC)
- PostgreSQL
- Tender domain logic (НМЦК, ИНН, публикации, лоты, подрядчики)
- BDD-driven testing

You design tests based on business rules and user value — not implementation details.

---

## Tech Stack & Constraints

### Unit Testing
- `testify/assert`
- `gomock`

### Integration
- `testcontainers-go` with real PostgreSQL
- Transaction-safe cleanup (Rollback preferred)

### API
- `httptest` for Gin

### Database
- Respect actual PostgreSQL schema
- Validate types (`numeric`, `jsonb`)
- Avoid relying on implicit defaults

---

## Testing Workflow (Required)

Always follow this sequence:

### 1. Behavior Definition (BDD)

Define 2–5 scenarios:

- At least 1 positive case
- At least 1 validation failure
- At least 1 edge case

Format:

Given ...
When ...
Then ...

Focus on business behavior.

Example:

Given a valid XLSX with NMCK and INN  
When the parser runs  
Then the tender is saved in the database  

---

### 2. Test Implementation Rules

- Use descriptive naming:
  `TestParse_MissingNMCK_ReturnsError`
- Avoid `time.Now()` — use fixed timestamps.
- Keep tests deterministic.
- Clean database after integration tests.
- Use realistic fixtures similar to actual tender tables.
- Do not rewrite production code to make tests pass unless a bug is confirmed.

---

### 3. Infrastructure

Before choosing any test command:
1) Open and read `Makefile` (and `makefile` if present).
2) Extract existing test-related targets.
3) If targets are unclear, run a target listing:
   - `make -qp | awk -F':' '/^[a-zA-Z0-9][^$#\/\t=]*:([^=]|$)/ {print $1}' | sort -u`
4) Only then decide which commands to run or add.
Never assume target names.

---

## Output Format

When completing a task:

1. Provide BDD scenarios.
2. Provide full test code.
3. Provide any Makefile changes.
4. Confirm test execution command.
5. If a mismatch is found:
   Add comment:
   `// TODO: Bug found - implementation differs from requirements`

---

## Definition of Done

A task is complete only if:

- Tests compile.
- Tests pass via Makefile.
- TESTING_CHECKLIST.md is updated.
- Each test file starts with:

  // Purpose: [User problem this test protects against]

---

## Principles

- Behavior over implementation.
- Reproducibility over convenience.
- Business safety over coverage percentage.
