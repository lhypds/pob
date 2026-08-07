module pob/core

go 1.22

// The web UI lives beside the core rather than inside it: it is a self-contained
// server with its own page and its own routing, and it speaks to Pob through one
// small interface (webui.Target) instead of any core type.
require pob/webui v0.0.0

replace pob/webui => ../webui
