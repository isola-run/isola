#!/bin/bash
# Check if sandbox ID is provided
if [ -z "$1" ]; then
  echo "Error: Sandbox ID is required"
  echo "Usage: $0 <SANDBOX_ID>"
  exit 1
fi

SANDBOX_ID="$1"
API_KEY="iso_sk_demo"
BASE_URL="http://localhost:30080/api/v1"
FILE_PATH="myfile.txt"  # Path inside sandbox to download (matches port-large-file.sh)
OUTPUT_FILE="downloaded_file.txt"  # Local output file

# Function to download large files via S3
download_large_file() {
  echo "=== Large File Download (via S3) ==="
  
  # Step 1: Request presigned download URL
  echo "Requesting presigned download URL..."
  RESPONSE=$(curl -s -X POST "${BASE_URL}/sandboxes/${SANDBOX_ID}/files/download-url" \
    -H "X-API-Key: ${API_KEY}" \
    -H "Content-Type: application/json" \
    -d "{
      \"path\": \"${FILE_PATH}\"
    }")

  echo "Response: $RESPONSE"
  
  DOWNLOAD_URL=$(echo "$RESPONSE" | jq -r '.download_url')
  DOWNLOAD_ID=$(echo "$RESPONSE" | jq -r '.download_id')
  
  if [ "$DOWNLOAD_URL" = "null" ] || [ -z "$DOWNLOAD_URL" ]; then
    echo "Error: Failed to get download URL"
    echo "$RESPONSE" | jq .
    exit 1
  fi

  # Replace internal k8s hostname with localhost for local testing
  DOWNLOAD_URL="${DOWNLOAD_URL//localstack.localstack.svc.cluster.local/localhost}"

  echo "Download URL: $DOWNLOAD_URL"
  echo "Download ID: $DOWNLOAD_ID"
  echo ""

  # Step 2: Download file from S3
  echo "Downloading file from S3..."
  curl -s -o "${OUTPUT_FILE}" "$DOWNLOAD_URL"
  
  echo "File saved to: ${OUTPUT_FILE}"
  ls -la "${OUTPUT_FILE}"
}

echo "Downloading file from sandbox ${SANDBOX_ID}"
echo "  Source path: ${FILE_PATH}"
echo "  Output file: ${OUTPUT_FILE}"
echo ""

if [ "$LARGE_FILE" = true ]; then
  download_large_file
else
  # Small file download (direct streaming)
  echo "=== Small File Download (direct) ==="
  
  # URL encode the path
  ENCODED_PATH=$(python3 -c "import urllib.parse; print(urllib.parse.quote('${FILE_PATH}'))")
  
  echo "Requesting file..."
  
  # Download file directly - response is now raw bytes (not JSON)
  # Use -w to get HTTP status code, -o to save body to file
  HTTP_CODE=$(curl -s -w "%{http_code}" -o "${OUTPUT_FILE}" \
    -X GET "${BASE_URL}/sandboxes/${SANDBOX_ID}/files?path=${ENCODED_PATH}" \
    -H "X-API-Key: ${API_KEY}")

  echo "HTTP status: $HTTP_CODE"
  
  if [ "$HTTP_CODE" != "200" ]; then
    # Error response is JSON - read it from the output file
    echo "Error downloading file:"
    cat "${OUTPUT_FILE}"
    echo ""
    
    # Check if it's a file size limit error - fall back to S3 download
    if grep -q "exceeds" "${OUTPUT_FILE}" 2>/dev/null; then
      echo ""
      echo "File too large for direct download, falling back to S3..."
      echo ""
      download_large_file
    else
      rm -f "${OUTPUT_FILE}"
      exit 1
    fi
  else
    echo "File saved to: ${OUTPUT_FILE}"
    ls -la "${OUTPUT_FILE}"
    echo ""
    echo "Content preview:"
    head -c 200 "${OUTPUT_FILE}"
    echo ""
  fi
fi

echo ""
echo "Done!"
