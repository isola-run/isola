#!/bin/bash
# Download file from sandbox - handles both small files (direct) and large files (via S3)
#
# Usage: ./download-file.sh <SANDBOX_ID> [FILE_PATH] [OUTPUT_FILE]
#
# For large files (>5MB), the gateway initiates an async upload to S3:
#   1. GET /files?path=X returns 202 with download_id
#   2. Poll GET /files?download_id=X until status="ready"
#   3. Download from presigned S3 URL

set -e

if [ -z "$1" ]; then
  echo "Error: Sandbox ID is required"
  echo "Usage: $0 <SANDBOX_ID> [FILE_PATH] [OUTPUT_FILE]"
  exit 1
fi

SANDBOX_ID="$1"
FILE_PATH="${2:-myfile.txt}"
OUTPUT_FILE="${3:-downloaded_file.txt}"
API_KEY="${API_KEY:-iso_sk_demo}"
BASE_URL="${BASE_URL:-http://localhost:30080/api/v1}"
POLL_INTERVAL=2
MAX_POLL_ATTEMPTS=60

# URL encode the path
encode_path() {
  python3 -c "import urllib.parse; print(urllib.parse.quote('$1'))"
}

# Replace internal k8s hostname with localhost for local testing
fix_url_for_local() {
  echo "${1//localstack.localstack.svc.cluster.local/localhost}"
}

# Poll for large file download to be ready
poll_for_download() {
  local download_id="$1"
  local attempt=0
  
  echo "Polling for download to be ready (id: ${download_id})..."
  
  while [ $attempt -lt $MAX_POLL_ATTEMPTS ]; do
    attempt=$((attempt + 1))
    
    RESPONSE=$(curl -s -X GET "${BASE_URL}/sandboxes/${SANDBOX_ID}/files?download_id=${download_id}" \
      -H "X-API-Key: ${API_KEY}")
    
    STATUS=$(echo "$RESPONSE" | jq -r '.status')
    
    if [ "$STATUS" = "ready" ]; then
      DOWNLOAD_URL=$(echo "$RESPONSE" | jq -r '.download_url')
      DOWNLOAD_URL=$(fix_url_for_local "$DOWNLOAD_URL")
      echo "Download ready!"
      echo ""
      
      echo "Downloading file from S3..."
      curl -s -o "${OUTPUT_FILE}" "$DOWNLOAD_URL"
      
      echo "File saved to: ${OUTPUT_FILE}"
      ls -la "${OUTPUT_FILE}"
      return 0
    elif [ "$STATUS" = "uploading" ]; then
      echo "  Attempt ${attempt}/${MAX_POLL_ATTEMPTS}: Still uploading..."
      sleep $POLL_INTERVAL
    else
      echo "Error: Unexpected status '$STATUS'"
      echo "$RESPONSE" | jq .
      return 1
    fi
  done
  
  echo "Error: Timeout waiting for download to be ready"
  return 1
}

echo "Downloading file from sandbox ${SANDBOX_ID}"
echo "  Source path: ${FILE_PATH}"
echo "  Output file: ${OUTPUT_FILE}"
echo ""

ENCODED_PATH=$(encode_path "$FILE_PATH")

# Request file download
echo "Requesting file..."
HTTP_CODE=$(curl -s -w "%{http_code}" -o /tmp/download_response.tmp \
  -X GET "${BASE_URL}/sandboxes/${SANDBOX_ID}/files?path=${ENCODED_PATH}" \
  -H "X-API-Key: ${API_KEY}")

echo "HTTP status: $HTTP_CODE"

case "$HTTP_CODE" in
  200)
    # Small file - direct download, response is raw bytes
    mv /tmp/download_response.tmp "${OUTPUT_FILE}"
    echo "File saved to: ${OUTPUT_FILE}"
    ls -la "${OUTPUT_FILE}"
    echo ""
    echo "Content preview:"
    head -c 200 "${OUTPUT_FILE}"
    echo ""
    ;;
    
  202)
    # Large file - async upload initiated, need to poll
    echo "Large file detected, async upload initiated"
    RESPONSE=$(cat /tmp/download_response.tmp)
    rm -f /tmp/download_response.tmp
    
    DOWNLOAD_ID=$(echo "$RESPONSE" | jq -r '.download_id')
    STATUS=$(echo "$RESPONSE" | jq -r '.status')
    
    if [ "$DOWNLOAD_ID" = "null" ] || [ -z "$DOWNLOAD_ID" ]; then
      echo "Error: No download_id in response"
      echo "$RESPONSE" | jq .
      exit 1
    fi
    
    echo "Download ID: $DOWNLOAD_ID"
    echo "Status: $STATUS"
    echo ""
    
    poll_for_download "$DOWNLOAD_ID"
    ;;
    
  *)
    # Error response
    echo "Error downloading file:"
    cat /tmp/download_response.tmp
    rm -f /tmp/download_response.tmp
    echo ""
    exit 1
    ;;
esac

echo ""
echo "Done!"
