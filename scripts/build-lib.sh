#!/bin/bash
# Builds the facelib C shared library.
#
# Usage:
#   scripts/build-lib.sh [target ...]
#
# Targets are GOOS/GOARCH pairs. With none given, only the host platform is
# built. Supported:
#   linux/amd64      -> lib/facelib/facelib-linux-amd64.so
#   windows/amd64    -> lib/facelib/facelib-windows-amd64.dll
#   darwin/amd64     -> lib/facelib/facelib-darwin-amd64.dylib
#   darwin/arm64     -> lib/facelib/facelib-darwin-arm64.dylib

set -euo pipefail

Dir="$(CDPATH="" cd -- "$(dirname -- "$0")/.." && pwd)"
SrcDir="$Dir/facelib"
OutDir="$Dir/lib/facelib"

# build identifier baked into the library and reported by fl_version
Version="${FACELIB_VERSION:-}"
if [ -z "$Version" ]; then
    Version="$(git -C "$Dir" describe --tags --always --dirty 2>/dev/null || true)"
fi

# when building from a tarball, or without git installed, we got nothing to describe
Version="${Version:-unknown}"
# Ensure the embedded model exists before the compiler warns about missing go:embed target
"$Dir/scripts/fetch-assets.sh"

mkdir -p "$OutDir"

build() {
    local goos="$1" goarch="$2" ext="$3" cc="$4"

    if [ -n "$cc" ] && ! command -v "$cc" > /dev/null 2>&1; then
        echo "Skipping $goos/$goarch: C compiler '$cc' not found." >&2
        return 1
    fi

    local out="$OutDir/facelib-$goos-$goarch.$ext"
    echo "Building $goos/$goarch -> lib/facelib/$(basename "$out")"

    # '-s -w' strip the symbol table and DWARF data cutting the bloat
    (
        cd "$SrcDir"
        env GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=1 \
            ${cc:+CC="$cc"} \
            go build -buildmode=c-shared \
                -ldflags "-s -w -X main.version=$Version" \
                -o "$out" .
    )

    # c-shared always emits a C header, python binding does not read it and
    # lib/ is packaged verbatim into the submod, so move it out to build/ where
    # all the generated artifacts live
    local header="${out%.*}.h"
    if [ -f "$header" ]; then
        mkdir -p "$Dir/build"
        mv -f "$header" "$Dir/build/facelib.h"
    fi

    # echo "  $(du -h "$out" | cut -f1)"
}

host_target() {
    local goos goarch
    goos="$(go env GOOS)"
    goarch="$(go env GOARCH)"
    echo "$goos/$goarch"
}

targets=("$@")
if [ ${#targets[@]} -eq 0 ]; then
    targets=("$(host_target)")
fi

failed=0
for target in "${targets[@]}"; do
    case "$target" in
        linux/amd64)
            build linux amd64 so "" || failed=1
            ;;
        windows/amd64)
            # Native builds on Windows use the toolchain already on PATH
            if [ "$(go env GOOS)" = "windows" ]; then
                build windows amd64 dll "" || failed=1
            else
                build windows amd64 dll x86_64-w64-mingw32-gcc || failed=1
            fi
            ;;
        darwin/amd64)
            build darwin amd64 dylib "" || failed=1
            ;;
        darwin/arm64)
            build darwin arm64 dylib "" || failed=1
            ;;
        *)
            echo "Unsupported target: $target" >&2
            failed=1
            ;;
    esac
done

# echo
# echo "Artifacts in lib/facelib:"
# # shellcheck disable=SC2012
# ls -lh "$OutDir" 2>/dev/null | tail -n +2 || true

exit "$failed"
