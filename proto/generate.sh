#!/usr/bin/env bash
# =====================================================================
# compkg proto stub generator (Linux / macOS / WSL)
# =====================================================================
# Output: stubs are written next to each .proto file (paths=source_relative).
# This matches the `option go_package` import path:
#   github.com/sidchai/compkg/proto/scheduler/v1
# =====================================================================
set -euo pipefail

PROTO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "[1/3] Checking toolchain..."
command -v protoc             >/dev/null || { echo "missing protoc";             exit 1; }
command -v protoc-gen-go      >/dev/null || { echo "missing protoc-gen-go";      exit 1; }
command -v protoc-gen-go-grpc >/dev/null || { echo "missing protoc-gen-go-grpc"; exit 1; }
echo "  protoc:             $(protoc --version)"
echo "  protoc-gen-go:      $(command -v protoc-gen-go)"
echo "  protoc-gen-go-grpc: $(command -v protoc-gen-go-grpc)"

echo "[2/3] Generating Go stubs (in-place, paths=source_relative)..."
cd "${PROTO_ROOT}"
PROTO_FILES=$(find . -name "*.proto" -type f | sed 's|^\./||')
for f in ${PROTO_FILES}; do echo "  -> ${f}"; done

protoc \
    --proto_path="${PROTO_ROOT}" \
    --go_out=. \
    --go_opt=paths=source_relative \
    --go-grpc_out=. \
    --go-grpc_opt=paths=source_relative \
    ${PROTO_FILES}

echo "[3/3] Done"
find "${PROTO_ROOT}" -name "*.pb.go" -type f | while read -r f; do
    rel="${f#${PROTO_ROOT}/}"
    size=$(du -k "${f}" | cut -f1)
    printf "  %-50s %5d KB\n" "${rel}" "${size}"
done
