module pob/core

go 1.22

// The Pob server lives beside the core rather than inside it: it is
// self-contained — its own routing, its own web UI — and it speaks to Pob
// through one small interface (server.Target) instead of any core type.
require pob/server v0.0.0

replace pob/server => ../server
