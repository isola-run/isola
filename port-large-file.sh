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
FILE_PATH="/tmp/largefile.bin"  
FILENAME="largefile.bin"
TARGET_PATH="myfile.txt"

# Step 1: Get presigned URL
echo "Requesting presigned URL..."
RESPONSE=$(curl -s -X POST "${BASE_URL}/sandboxes/${SANDBOX_ID}/files/upload-url" \
  -H "X-API-Key: ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d "{
    \"path\": \"${TARGET_PATH}\",
    \"filename\": \"${FILENAME}\",
    \"content_type\": \"application/octet-stream\"
  }") 

UPLOAD_URL=$(echo "$RESPONSE" | jq -r '.upload_url')
UPLOAD_ID=$(echo "$RESPONSE" | jq -r '.upload_id')

UPLOAD_URL="${UPLOAD_URL//localstack.localstack.svc.cluster.local/localhost}"

echo "Upload URL: $UPLOAD_URL"
echo "Upload ID: $UPLOAD_ID"

echo "Uploading file to S3..."
curl -X PUT "$UPLOAD_URL" \
  -H "Content-Type: application/octet-stream" \
  --data-binary "@${FILE_PATH}"


# Step 3: Confirm
echo "Confirming upload..."
curl -X POST "${BASE_URL}/sandboxes/${SANDBOX_ID}/files/confirm" \
  -H "X-API-Key: ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d "{
    \"upload_id\": \"${UPLOAD_ID}\",
    \"filename\": \"${FILENAME}\",
    \"path\": \"${TARGET_PATH}\"
  }"

echo "Done!"