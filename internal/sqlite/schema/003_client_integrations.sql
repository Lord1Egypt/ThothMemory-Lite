-- ThothMemory Lite schema 3: observed client integrations.
-- One row per credential and MCP client implementation, written when a real
-- MCP session initializes (client name and version as the client reports them)
-- and when it ends after at least one successful tool call. This is operational
-- state, not history: rows are updated in place and hold no memory content.
CREATE TABLE client_integrations(principal_id TEXT NOT NULL REFERENCES principals(id), client_name TEXT NOT NULL CHECK(length(client_name) BETWEEN 1 AND 200), client_version TEXT NOT NULL DEFAULT '' CHECK(length(client_version)<=100), transport TEXT NOT NULL CHECK(transport IN ('stdio','in-process')), first_seen_at TEXT NOT NULL, last_handshake_at TEXT NOT NULL, last_operation_at TEXT, handshakes INTEGER NOT NULL DEFAULT 1 CHECK(handshakes>0), PRIMARY KEY(principal_id,client_name)) STRICT;
