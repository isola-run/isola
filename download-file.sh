#!/bin/bash
SANDBOX_ID="0ed17dff-a155-4194-b45b-8427afbea9a1"
API_KEY="iso_sk_demo"
BASE_URL="http://localhost:30080"
FILE_PATH="myfile.txt"  # Path inside sandbox to download (matches port-large-file.sh)
OUTPUT_FILE="downloaded_file.txt"  # Local output file

# Parse command line args
LARGE_FILE=false
while [[ "$#" -gt 0 ]]; do
  case $1 in
    --large) LARGE_FILE=true ;;
    --path) FILE_PATH="$2"; shift ;;
    --output) OUTPUT_FILE="$2"; shift ;;
    --sandbox) SANDBOX_ID="$2"; shift ;;
    *) echo "Unknown parameter: $1"; exit 1 ;;
  esac
  shift
done

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
  # Small file download (direct)
  echo "=== Small File Download (direct) ==="
  
  # URL encode the path
  ENCODED_PATH=$(python3 -c "import urllib.parse; print(urllib.parse.quote('${FILE_PATH}'))")
  
  echo "Requesting file..."
  RESPONSE=$(curl -s -X GET "${BASE_URL}/sandboxes/${SANDBOX_ID}/files?path=${ENCODED_PATH}" \
    -H "X-API-Key: ${API_KEY}")

  # Check for error
  ERROR=$(echo "$RESPONSE" | jq -r '.error // empty')
  if [ -n "$ERROR" ]; then
    MESSAGE=$(echo "$RESPONSE" | jq -r '.message // empty')
    
    # Check if it's a file size limit error - fall back to S3 download
    if echo "$MESSAGE" | grep -q "exceeds direct download limit"; then
      echo "Error: $ERROR"
      echo "$RESPONSE" | jq .
      echo ""
      echo "File too large for direct download, falling back to S3..."
      echo ""
      download_large_file
    else
      echo "Error: $ERROR"
      echo "$RESPONSE" | jq .
      exit 1
    fi
  else
    # Extract and decode base64 content
    CONTENT=$(echo "$RESPONSE" | jq -r '.content')
    SIZE=$(echo "$RESPONSE" | jq -r '.size')
    PATH_RESP=$(echo "$RESPONSE" | jq -r '.path')
    
    echo "File path: $PATH_RESP"
    echo "File size: $SIZE bytes"
    
    # Decode base64 content and save to file
    echo "$CONTENT" | base64 -d > "${OUTPUT_FILE}"
    
    echo "File saved to: ${OUTPUT_FILE}"
    ls -la "${OUTPUT_FILE}"
  fi
fi

echo ""
echo "Done!"
