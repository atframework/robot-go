#!/bin/sh
# POSIX counterpart of generate-config-code.ps1: generate config code into a staging
# directory, then content-stable publish into the output directory.
set -e

PYTHON_BIN="$1"
GEN_SCRIPT="$2"
TEMPLATE_DIR="$3"
RES_CONFIG_PB="$4"
IMPORT_PATH="$5"
OUTPUT_DIR="$6"
STAGING_DIR="$7"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

if [ -z "$PYTHON_BIN" ] || [ -z "$GEN_SCRIPT" ] || [ -z "$TEMPLATE_DIR" ] || [ -z "$RES_CONFIG_PB" ] || [ -z "$IMPORT_PATH" ] || [ -z "$OUTPUT_DIR" ] || [ -z "$STAGING_DIR" ]; then
  echo "Usage: generate-config-code.sh <python-bin> <xrescode-gen.py> <template-dir> <res-config-pb> <import-path> <output-dir> <staging-dir>" >&2
  exit 1
fi
if [ -d "$STAGING_DIR" ]; then
  STAGING_RESOLVED=$(cd "$STAGING_DIR" && pwd -P)
  if [ "$STAGING_RESOLVED" = "/" ]; then
    echo "Refusing unsafe staging dir: $STAGING_DIR" >&2
    exit 1
  fi
fi
if [ -d "$OUTPUT_DIR" ]; then
  OUTPUT_RESOLVED=$(cd "$OUTPUT_DIR" && pwd -P)
  if [ "$OUTPUT_RESOLVED" = "/" ]; then
    echo "Refusing unsafe output dir: $OUTPUT_DIR" >&2
    exit 1
  fi
fi

rm -rf "$STAGING_DIR"
mkdir -p "$STAGING_DIR"
STAGING_DIR=$(cd "$STAGING_DIR" && pwd -P)
if [ "$STAGING_DIR" = "/" ]; then
  echo "Refusing unsafe staging dir after resolution" >&2
  exit 1
fi

"$PYTHON_BIN" "$GEN_SCRIPT" -i "$TEMPLATE_DIR" -p "$RES_CONFIG_PB" -o "$STAGING_DIR" '--set' "config_protocol_import_path=$IMPORT_PATH" -g config_group.go.mako:config_group.go -g config_manager.go.mako:config_manager.go -l 'config_set.go.mako:${"config_set_{0}.go".format(loader.get_go_pb_name())}' -t server

sh "$SCRIPT_DIR/publish-directory.sh" "$STAGING_DIR" "$OUTPUT_DIR"
