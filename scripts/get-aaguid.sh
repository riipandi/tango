#!/usr/bin/env bash

# 1. Detect and check the operating system
OS_TYPE="$(uname -s)"
CURL_OPTS=("-sfL")

case "$OS_TYPE" in
Linux*)
    echo "[INFO] System: Linux environment verified"
    ;;
Darwin*)
    echo "[INFO] System: macOS (Darwin) environment verified"
    ;;
*)
    echo "[ERROR] Unsupported Operating System ($OS_TYPE)"
    echo "[INFO] This script is strictly designed for Linux and macOS environments."
    exit 1
    ;;
esac

# 2. Check if curl is available before proceeding
if ! command -v curl &>/dev/null; then
    echo "[ERROR] The 'curl' dependency is missing"
    echo "[INFO] Please install curl first (e.g., 'sudo apt install curl' or 'brew install curl')."
    exit 1
fi

# 3. Determine project directory paths dynamically based on script location
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
OUTPUT_FILE="$PROJECT_DIR/storage/aaguid.json"
TEMP_FILE="$PROJECT_DIR/storage/aaguid.tmp.json"

# The raw endpoint for the combined data stream
URL="https://raw.githubusercontent.com/passkeydeveloper/passkey-authenticator-aaguids/main/combined_aaguid.json"

echo "[INFO] Fetching the latest WebAuthn AAGUID definitions..."

# 4. Ensure the storage directory exists
mkdir -p "$PROJECT_DIR/storage"

# 5. Download the new data into a temporary file using optimized flags
if ! curl "${CURL_OPTS[@]}" -o "$TEMP_FILE" "$URL"; then
    echo "[ERROR] Network data synchronization failed"
    echo "[INFO] Troubleshoot:"
    echo "       - Check your local internet connectivity."
    echo "       - Verify that the GitHub raw asset endpoint is reachable."
    rm -f "$TEMP_FILE"
    exit 1
fi

# 6. Validate the JSON file structure using 'jq' if available
if command -v jq &>/dev/null; then
    # Validate that it is a healthy JSON object and contains keys
    if ! jq -e '. | objects and (keys | length > 0)' "$TEMP_FILE" >/dev/null 2>&1; then
        echo "[WARN] Data validation failed (Malformed JSON or empty array received)"
        echo "[INFO] System fallback triggered: Retaining previous data to avoid system disruption."
        rm -f "$TEMP_FILE"
        exit 1
    fi
else
    # Fallback verification using grep if 'jq' is missing
    if [ ! -s "$TEMP_FILE" ] || grep -q '^[[:space:]]*{[[:space:]]*}[[:space:]]*$' "$TEMP_FILE"; then
        echo "[WARN] Data validation failed (File is empty or contains only brackets)"
        echo "[INFO] System fallback triggered: Retaining previous data to avoid system disruption."
        rm -f "$TEMP_FILE"
        exit 1
    fi
fi

# 7. Swap the temp file with the output file if all checks pass
mv "$TEMP_FILE" "$OUTPUT_FILE"
FILE_SIZE=$(ls -lh "$OUTPUT_FILE" | awk '{print $5}')
RELATIVE_PATH="${OUTPUT_FILE#$PROJECT_DIR/}"
echo "[SUCCESS] AAGUID payload refreshed successfully"
echo "[INFO] Path: $RELATIVE_PATH"
echo "[INFO] Size: $FILE_SIZE"
