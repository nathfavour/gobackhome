# System Architecture: Sovereign Application Engine

## System Vision & Manifesto

We are building a **Sovereign Application Engine**. It is a single-binary, multi-tenant Backend-as-a-Service (BaaS) and Full-Stack Runtime engineered in pure Go. It starts with the minimalist footprint of embedded, zero-RAM SQLite databases per project, and dynamically morphs into an enterprise-grade PostgreSQL cluster without a single line of application code changing. 

This document details exactly **what** has been implemented so far, and crucially, **why** those architectural decisions were made.

---

## 1. Multi-Module Go Workspace Topology

### What We Built
We initialized the repository as a Go Workspace (`go.work`) with strictly separated sub-modules instead of a standard single-module repository.
*   **`core/kernel`**: The system orchestrator.
*   **`core/ports`**: Pure interfaces defining the domain boundary.
*   **`adapters/storage_sqlite`**: The SQLite implementation.
*   **`adapters/storage_postgres`**: The PostgreSQL implementation.
*   **Other stubbed adapters & runtimes**: `compute_sandbox`, `network_http`, `server`, `frontend_vhost`.

### Why We Built It This Way
*   **Enforced Decoupling (The Dependency Rule):** By physically separating these into Go modules, we make it impossible for the `core` to accidentally import `github.com/mattn/go-sqlite3` or `github.com/lib/pq`. If a developer attempts to bleed an adapter dependency into the core business logic, the Go compiler will hard-fail. 
*   **Hexagonal Architecture:** This strict physical boundary forces all inbound and outbound communication to cross through the `core/ports` interfaces, ensuring our "Morphic" database swapping remains viable.

---

## 2. Core Ports (The Domain Boundary)

### What We Built
In `core/ports`, we defined:
1.  `StorageEngine`: A pure Go interface defining `InitializeTenant`, `ExecuteMigration`, and standard CRUD operations.
2.  `SchemaBlueprint`: A JSON-serializable representation of a database schema (collections, fields, types).

### Why We Built It This Way
*   **Abstracting the SQL Dialect:** The core engine cannot know if it is talking to SQLite, Postgres, or a future driver like MySQL. The `SchemaBlueprint` acts as an AST (Abstract Syntax Tree). The underlying adapters are responsible for translating this generic blueprint into highly optimized, dialect-specific native DDL (e.g., SQLite `STRICT` mode vs. Postgres `JSONB` native types).
*   **Driver Swappability:** Because both SQLite and Postgres adapters implement the exact same `StorageEngine` interface, the Kernel can swap the underlying pointer at runtime without the application layer dropping a single request.

---

## 3. The Morphic Storage Engine: SQLite Adapter

### What We Built
The `adapters/storage_sqlite` module manages data using single-file SQLite databases per tenant.
*   **Tenant Worker Loop (`worker.go`):** We implemented a Go channel multiplexer (`chan writeTask`) that binds exactly one goroutine to execute all write transactions for a specific tenant.
*   **WAL Mode & Concurrency:** We configured SQLite with `_journal_mode=WAL` and allowed `SetMaxOpenConns(10)`.

### Why We Built It This Way
*   **Solving SQLite's Writer Bottleneck:** SQLite strictly allows only *one concurrent writer* at a time. If we allowed arbitrary HTTP requests to trigger `db.Exec()` concurrently, we would hit `database is locked` errors under load.
*   **Zero Global Lock Contention:** Instead of using a global `sync.Mutex` across the entire engine (which would make one tenant's heavy write block *all* other tenants), we isolate the locks. The dedicated worker loop processes writes sequentially per tenant.
*   **Concurrent Reads:** Because we enabled WAL (Write-Ahead Logging), reads can happen concurrently with the single background writer. Therefore, `Select` and `FindByID` bypass the channel queue and execute directly against the connection pool for maximum speed.

---

## 4. The Morphic Storage Engine: PostgreSQL Adapter

### What We Built
The `adapters/storage_postgres` module connects to an external PostgreSQL cluster.
*   **Isolated Schemas:** When `InitializeTenant` is called, it executes `CREATE SCHEMA IF NOT EXISTS "tenant_id";`. All subsequent tables for that tenant are created inside that specific schema.
*   **High-Concurrency Execution:** Unlike SQLite, the Postgres adapter does *not* use a worker queue; it executes writes concurrently directly against the `*sql.DB` pool.

### Why We Built It This Way
*   **Multi-Tenancy at Scale:** Running thousands of individual PostgreSQL databases on a single cluster incurs massive overhead (processes, shared buffers). By using a single database but isolating tenants via `SCHEMA`, we achieve logical data isolation with vastly lower memory overhead.
*   **Unlocking Concurrency:** Postgres handles row-level locking natively. By removing the channel multiplexer used in SQLite, we allow Postgres to fully utilize its high-concurrency connection pooling, making it the perfect target for when a project outgrows the embedded SQLite constraints.

---

## 5. Core Kernel Orchestrator (The Hot-Swap Engine)

### What We Built
The `core/kernel` module contains the `Orchestrator` and `TenantContext`.
*   **Atomic State & Pointer Management:** We track tenant state and their active storage engine using `atomic.Value` inside `TenantContext`.
*   **Zero-Mutex Registry:** The registry of all tenants is held in a `sync.Map`.
*   **The `CoordinateMigration` Pipeline:** Implemented the 4-phase sequence to hot-swap a tenant from SQLite to Postgres.

### Why We Built It This Way
*   **Zero-Downtime Swapping:** When transitioning a tenant from SQLite to Postgres, we need absolute lock-free performance on the read-path. By using `atomic.Value` to store the active `StorageEngine` interface, the HTTP router can fetch the current DB pointer in nanoseconds without blocking, even while a migration is spinning up in the background.
*   **The 4-Phase Pipeline Strategy:**
    1.  **Intercept (StateMigrating):** We atomically flag the tenant. The soon-to-be-built HTTP layer will see this state and briefly pause/buffer incoming writes in memory.
    2.  **Target Verification:** We initialize the Postgres schema and generate the DDL on the fly.
    3.  **Stream & Pipe:** We stream records out of the SQLite source and pipe them into Postgres via the uniform port interface.
    4.  **Swap & Purge:** We atomically swap the `StorageEngine` pointer. Any queued HTTP requests are released and instantly hit Postgres. SQLite is decommissioned.

## Next Steps
With the core backend data-tier proven and complete, the architecture is ready to integrate the network edge (`network_http` for HTTP/WebSocket multiplexing and in-memory write buffering) and the compute layer (`compute_sandbox` for isolated serverless functions).
