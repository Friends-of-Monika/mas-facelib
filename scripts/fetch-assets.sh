#!/bin/bash
# Fetches the binary assets embedded into the shared library.
set -euo pipefail

ModelUrl="https://media.githubusercontent.com/media/onnx/models/main/validated/vision/body_analysis/emotion_ferplus/model/emotion-ferplus-8.onnx"
ModelSha256="a2a2ba6a335a3b29c21acb6272f962bd3d47f84952aaffa03b60986e04efa61c"

Dir="$(CDPATH="" cd -- "$(dirname -- "$0")/.." && pwd)"
SrcDir="$Dir/facelib"
DataDir="$SrcDir/internal/data"

CascadePath="$DataDir/facefinder"
ModelPath="$DataDir/emotion-ferplus-8.onnx"

mkdir -p "$DataDir"


# pigo face detection (cascade)

fetch_cascade() {
    # resolve the pinned pigo version through the module graph rather than
    # hardcoding it, so bumping go.mod is the only edit a version change needs
    local pigoDir
    if ! pigoDir="$(cd "$SrcDir" && go list -m -f '{{.Dir}}' github.com/esimov/pigo 2>/dev/null)" \
        || [ -z "$pigoDir" ]; then
        echo "Downloading esimov/pigo module..."
        (cd "$SrcDir" && go mod download github.com/esimov/pigo)
        pigoDir="$(cd "$SrcDir" && go list -m -f '{{.Dir}}' github.com/esimov/pigo)"
    fi

    local src="$pigoDir/cascade/facefinder"
    if [ ! -f "$src" ]; then
        echo "Cascade not found in pigo module at $src" >&2
        return 1
    fi

    if [ -f "$CascadePath" ] && cmp -s "$src" "$CascadePath"; then
        echo "Cascade already present and up to date."
        return 0
    fi

    cp -f "$src" "$CascadePath"
    # note: module cache files are read-only; the copy should not inherit that
    chmod 644 "$CascadePath"
    echo "Cascade copied from $(basename "$pigoDir")."
}


# onnx emotion model

# have to resort to this trick because bsd sha256sum sucks
verify_model() {
    [ -f "$ModelPath" ] || return 1
    [ "$(sha256sum "$ModelPath" | cut -d' ' -f1)" = "$ModelSha256" ]
}

fetch_model() {
    if verify_model; then
        echo "Emotion model already present and verified."
        return 0
    fi

    if [ -f "$ModelPath" ]; then
        echo "Emotion model present but checksum does not match; re-downloading."
    fi

    echo "Downloading emotion model (33 MB)..."

    # download to a temporary file so an interrupted transfer never leaves a
    # corrupt model in place that the next build would happily embed
    local temp
    temp="$(mktemp)"
    trap 'rm -f "$temp"' RETURN

    if command -v curl > /dev/null 2>&1; then
        curl -fL --progress-bar -o "$temp" "$ModelUrl"
    elif command -v wget > /dev/null 2>&1; then
        wget -q --show-progress -O "$temp" "$ModelUrl"
    else
        echo "Cannot download model: curl/wget is not available on PATH." >&2
        return 1
    fi

    local actual
    actual="$(sha256sum "$temp" | cut -d' ' -f1)"

    if [ "$actual" != "$ModelSha256" ]; then
        echo "Checksum mismatch on downloaded model, not going to install it." >&2
        echo "  expected: $ModelSha256" >&2
        echo "  actual:   $actual" >&2
        return 1
    fi

    mv "$temp" "$ModelPath"
    echo "Emotion model saved to facelib/internal/data/emotion-ferplus-8.onnx"
}

# fetch everything
fetch_cascade
fetch_model
