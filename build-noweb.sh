#!/usr/bin/env bash
set -euo pipefail

mkdir -p bin
go build -o bin/easymatrix ./cmd/server

case "$(go env GOOS)" in
  darwin)
    LIB_SUFFIX="dylib"
    ;;
  windows)
    LIB_SUFFIX="dll"
    ;;
  *)
    LIB_SUFFIX="so"
    ;;
esac

go build -buildmode=c-shared -o "bin/libeasymatrixffi.${LIB_SUFFIX}" ./cmd/ffi
