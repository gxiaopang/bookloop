# BookLoop Development Guide

## 1. Project

BookLoop is a campus second-hand book marketplace and order management platform.

The project is intended both as a real full-stack application and as a learning project for Go backend development.

Development environment:

- Windows + WSL2 Ubuntu
- Go 1.27+
- Node.js 20+
- Docker Desktop with WSL integration
- Git

Deployment workflow:

WSL local development
→ local testing
→ Docker Compose testing
→ git push
→ cloud server git pull
→ Docker Compose deployment
→ Nginx reverse proxy

---

## 2. Tech Stack

Backend:

- Go
- Gin
- GORM
- MySQL 8
- Redis
- JWT
- bcrypt

Frontend:

- Vue 3
- TypeScript
- Vite
- Element Plus
- Pinia
- Axios

Infrastructure:

- Docker
- Docker Compose
- Nginx

Do not introduce new frameworks or infrastructure unless there is a clear requirement.

---

## 3. Architecture

The first version MUST use a modular monolith.

Do NOT introduce:

- microservices
- Kubernetes
- Kafka
- Elasticsearch
- real payment integrations
- unnecessarily complex distributed-system patterns

Backend request flow should normally follow:

Router
→ Handler
→ Service
→ Repository
→ Database

Responsibilities:

### Handler

Responsible for:

- parsing HTTP requests
- parameter validation
- retrieving authenticated user context
- calling Service
- returning HTTP responses

Handlers MUST NOT directly operate on the database.

### Service

Responsible for:

- business logic
- permission checks
- transaction orchestration
- business validation

### Repository

Responsible for:

- database access
- GORM queries
- persistence operations

Repository code should not contain HTTP-specific logic.

### Model

Represents database persistence structures.

### DTO

Represents request and response structures.

Do not expose database models directly as API response objects when a DTO is more appropriate.

---

## 4. Backend Directory Convention

Prefer the following structure:

backend/
├── cmd/
│   └── server/
├── internal/
│   ├── config/
│   ├── handler/
│   ├── service/
│   ├── repository/
│   ├── model/
│   ├── dto/
│   ├── middleware/
│   └── router/
├── pkg/
├── migrations/
├── tests/
├── Dockerfile
├── go.mod
└── go.sum

Do not create unnecessary abstraction layers.

---

## 5. Core Business Rules

The system contains these main domains:

- User
- Book
- Favorite
- Order
- Admin

Important business constraints:

1. A second-hand book can only be successfully sold once.

2. If two users attempt to purchase the same book concurrently, only one purchase may succeed.

3. Order creation will use MySQL transactions and row-level locking where appropriate.

4. A user cannot purchase their own book.

5. A seller can only modify books they own.

6. Authentication uses JWT.

7. Passwords must never be stored in plaintext.

8. Order creation should eventually support idempotency using Redis and an Idempotency-Key.

9. The first version uses simulated payment only.

10. Do not implement real payment providers.

---

## 6. Coding Principles

Prefer:

- simple code
- explicit code
- readable code
- idiomatic Go
- small focused functions
- clear error handling
- dependency injection through constructors where useful

Avoid:

- unnecessary interfaces
- premature generic abstractions
- excessive design patterns
- global mutable state
- hidden side effects
- copy-pasted business logic

Do not create an interface merely because a concrete type exists.

Create abstractions only when they provide a clear benefit.

---

## 7. Go Rules

All Go changes should pass:

go fmt ./...
go vet ./...
go test ./...

Use context.Context for database and service operations where appropriate.

Errors must be handled explicitly.

Do not silently ignore errors.

Do not use panic for normal business errors.

Business errors and internal errors should be distinguishable.

Use transactions for operations that modify multiple related records atomically.

---

## 8. Database Rules

Use MySQL as the source of truth.

Redis is auxiliary infrastructure and must not become the authoritative data store for core business data.

Database schema changes should be explicit and reproducible.

Think carefully about:

- primary keys
- unique constraints
- foreign keys where appropriate
- indexes
- transaction boundaries
- concurrent updates

For concurrency-sensitive order logic, correctness is more important than premature optimization.

---

## 9. Redis Rules

Redis should only be introduced for concrete use cases.

Expected initial use cases include:

- request idempotency
- selected cache scenarios
- token blacklist if logout revocation is implemented
- hot-data statistics if needed later

Do not cache everything by default.

Do not add distributed locks unless database transactions cannot correctly solve the problem.

---

## 10. API Rules

Use REST-style APIs.

API prefix:

/api/v1

Use consistent JSON responses and HTTP status codes.

Validate all external input.

Never trust identifiers or ownership information supplied by the frontend.

For example, seller_id should normally come from authenticated user context rather than directly trusting a client-provided seller_id.

Authentication header format:

Authorization: Bearer <token>

---

## 11. Security

Never commit secrets.

The following must not be committed:

- .env
- JWT secrets
- database passwords
- production credentials
- private keys
- cloud server credentials

Provide .env.example instead.

Production MySQL and Redis ports must not be exposed publicly unless explicitly required.

Validate user input.

Hash passwords using bcrypt or another approved password hashing mechanism.

---

## 12. Testing

Each development phase should be independently testable.

Priority should be given to testing:

- Service business logic
- permission rules
- order state transitions
- concurrent purchase behavior
- idempotency
- repository queries where important

Do not claim that a feature works without actually running relevant tests or commands.

After backend changes, run:

go fmt ./...
go vet ./...
go test ./...

Report failures instead of hiding them.

---

## 13. Development Workflow

Do not implement the whole project at once.

The project should progress in phases:

Phase 0: System design
Phase 1: Go backend infrastructure
Phase 2: User and authentication
Phase 3: Book marketplace
Phase 4: Favorites
Phase 5: Orders
Phase 6: Redis enhancements
Phase 7: Vue frontend
Phase 8: Dockerization
Phase 9: Cloud deployment
Phase 10: CI/CD

Only work on the requested phase.

Do not automatically start the next phase after finishing the current phase.

At the end of each phase:

1. summarize changed files
2. explain important design decisions
3. provide verification commands
4. run relevant tests where possible
5. stop and wait for confirmation

---

## 14. Learning Requirement

This is also a learning project.

When implementing important functionality, favor code that can be clearly explained in an interview.

For important backend mechanisms such as:

- JWT authentication
- middleware
- database transactions
- SELECT FOR UPDATE
- Redis idempotency
- cache consistency
- graceful shutdown

explain:

- why it is needed
- how it works
- request execution flow
- alternatives considered
- important failure cases

Do not hide complexity behind generated code without explanation.

---

## 15. Change Discipline

Before modifying code:

1. inspect relevant existing files
2. understand existing architecture
3. make the smallest coherent change

Do not rewrite unrelated code.

Do not rename large parts of the project unless necessary.

Do not delete existing functionality without explicit justification.

If requirements conflict with docs/PLAN.md, point out the conflict before making a major architectural change.

---

## 16. Git

Keep commits focused.

Suggested commit style:

feat: add user authentication
feat: add book publishing
fix: prevent duplicate order creation
refactor: extract order repository
test: add order concurrency tests
docs: update API design
chore: add docker configuration

Never commit secrets or generated dependency directories such as node_modules.
