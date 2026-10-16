-- 0001_init.sql — skema awal Phase 1: tenants, agents, capabilities.
--
-- Catatan desain yang sengaja dibuat eksplisit di sini:
--   * UUID dibuat di sisi Go (uuid.New()), bukan oleh DEFAULT di PostgreSQL,
--     supaya ID sudah ada sebelum INSERT dan bisa dipakai untuk logging dan
--     response tanpa round-trip tambahan.
--   * Semua kolom waktu memakai TIMESTAMPTZ. TIMESTAMP polos tidak menyimpan
--     zona waktu dan diam-diam berarti "waktu lokal server", yang berbeda
--     antara container, laptop, dan CI.

CREATE TABLE tenants (
    id         UUID PRIMARY KEY,
    slug       TEXT        NOT NULL UNIQUE,
    name       TEXT        NOT NULL,
    status     TEXT        NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE agents (
    id         UUID PRIMARY KEY,
    tenant_id  UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name       TEXT        NOT NULL,
    endpoint   TEXT        NOT NULL,
    status     TEXT        NOT NULL DEFAULT 'unknown',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Multi-tenancy ditegakkan oleh database, bukan oleh kode aplikasi.
    -- Dua tenant boleh sama-sama punya "security-agent"; satu tenant tidak
    -- boleh punya dua. Index inilah yang memenangkan perlombaan antara dua
    -- request POST bersamaan — pengecekan "cek dulu, baru insert" di kode
    -- aplikasi tidak pernah bisa menjamin hal itu.
    CONSTRAINT agents_tenant_name_key UNIQUE (tenant_id, name)
);

CREATE TABLE capabilities (
    id         UUID        PRIMARY KEY,
    agent_id   UUID        NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    name       TEXT        NOT NULL,
    version    TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT capabilities_agent_name_version_key UNIQUE (agent_id, name, version)
);

-- Jalur panas router di Phase 3: "agent mana di tenant X yang sehat dan
-- menyediakan kapabilitas Y?". Dua index ini yang melayani join tersebut.
CREATE INDEX idx_capabilities_name    ON capabilities (name);
CREATE INDEX idx_agents_tenant_status ON agents (tenant_id, status);

-- Mendukung paginasi cursor yang stabil: urutan (created_at, id) harus
-- bisa di-seek, bukan di-scan.
CREATE INDEX idx_agents_tenant_created ON agents (tenant_id, created_at, id);
CREATE INDEX idx_tenants_created       ON tenants (created_at, id);
