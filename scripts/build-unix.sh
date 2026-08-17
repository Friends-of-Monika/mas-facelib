#!/bin/bash

# This script creates a .zip package with your submod files.
# Below you can configure some of its parameters, but generally,
# unless you know what you're doing, DO NOT change actual code.


# CLI -header parameter
while [ "$#" -gt 0 ]; do
    case "$1" in
        -h|--header)
            shift
            Header="$1"
            break
        ;;
    esac
done

# Whether to create enclosing submod folder or not (usually you'd keep it true)
CreateSubmodFolder=true

RootPathFormat="%s"
PrefixPath="game/Submods"
LibPrefixPath="game/python-packages"
ExcludeNames=(".gitkeep" "README.md" "*.pyc" "__pycache__" "test_*.py" "testdata")
ZipFileFormat="%s-%s.zip"

ProjectScriptsDir="scripts"
ProjectBuildDir="build"
ProjectModDir="mod"
ProjectLibDir="lib"


# Locate script file location and create temp directory
Dir="$(CDPATH="" cd -- "$(dirname -- "$0")" && pwd)"
Temp="$(mktemp -d)"

if [ -z "$Header" ]; then
    # Locate header files automatically and parse JSON
    Headers="$(python "$Dir/../$ProjectScriptsDir/find_header.py" find \
        "$Dir/../$ProjectModDir" | jq -r 'to_entries|map("\(.key)=\(.value|tostring)")|.[]')"

    # Check that we have at least some of them
    if [ -z "$Headers" ]; then
        echo "Cannot build submod! Found no valid header files."
        exit 2
    fi

    # But not more than one, so we can't choose
    HeaderCount="$(echo "$Headers" | wc -l)"
    if [ "$HeaderCount" -gt 1 ]; then
        echo "Cannot build submod! Found too many valid header files."
        echo "Run this script with --header parameter instead."
        exit 2
    fi

    # Save header values to Submod
    HeaderPath="$(echo "$Headers" | cut -d'=' -f1)"
    echo "Found valid submod header in $HeaderPath!"
    SubmodName="$(echo "$Headers" | cut -d'=' -f2- | jq -r '.name')"
    SubmodVersion="$(echo "$Headers" | cut -d'=' -f2- | jq -r '.version')"

else
    # Parse header from the provided header .rpy script and parse JSON
    if ! Submod="$(python "$Dir/../$ProjectScriptsDir/find_header.py" header "$Header" \
        2> /dev/null | jq -r '.')";
    then
        echo "Invalid header file: $Header!"
        exit 2
    fi

    echo "Using provided submod header in $Header."
    SubmodName="$(echo "$Submod" | jq -r '.name')"
    SubmodVersion="$(echo "$Submod" | jq -r '.version')"
fi

# Create build directory
Build="$Dir/../$ProjectBuildDir"
mkdir -p "$Build"

# Variables in ZipFileFormat
Name="$(echo "$SubmodName" | tr '[:upper:]' '[:lower:]' | tr -s ' ' '-')"
Version="$(echo "$SubmodVersion" | tr '[:upper:]' '[:lower:]' | tr -s ' ' '-')"
# Package file name
# shellcheck disable=SC2059
Package=$(printf "$ZipFileFormat" "$Name" "$Version")

echo "Packaging $SubmodName $SubmodVersion..."
echo "Created .zip will be saved as $ProjectBuildDir/$Package."

# Top-level folder everything else goes under
Root="$Temp"
if [ -n "$RootPathFormat" ]; then
    # shellcheck disable=SC2059
    Root="$Temp/$(printf "$RootPathFormat" "$SubmodName")"
fi

# Create mod folder with prefix
Mod="$Root/$PrefixPath"
if [ "$CreateSubmodFolder" = "true" ]; then Mod="$Mod/$SubmodName"; fi
mkdir -p "$Mod"

# Copy mod files, optionally copy lib/ into its own prefix
cp -r "$Dir/../$ProjectModDir/"* "$Mod"
if [ -d "$Dir/../$ProjectLibDir" ]; then
    Lib="$Root/$LibPrefixPath"
    mkdir -p "$Lib"
    cp -r "$Dir/../$ProjectLibDir/"* "$Lib"
fi

# Drop excluded files
ExcludeArgs=()
for Pattern in "${ExcludeNames[@]}"; do
    ExcludeArgs+=(-o -iname "$Pattern")
done
find "$Temp" -depth \( "${ExcludeArgs[@]:1}" \) -exec rm -rf {} +

# Create .zip, remove temp folder
(cd "$Temp" && find . | zip -9@q "$Build/$Package")
rm -rf "$Temp"
