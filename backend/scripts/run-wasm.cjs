// Optional runner: node scripts/run-wasm.cjs <GOROOT>/lib/wasm/wasm_exec_node.js tests.wasm
// Use Go's in-memory net implementation for httptest, not Node's global fetch.
globalThis.fetch = undefined;
process.argv.splice(1, 1);
require(process.argv[1]);
