#!/bin/bash
# Isola Quick Start Example
# This script demonstrates basic sandbox operations

set -e

# Configuration
API="${ISOLA_API:-http://localhost:8080}"
API_KEY="${ISOLA_API_KEY:-iso_sk_demo}"

echo "=== Isola Quick Start ==="
echo "API: $API"
echo ""

# Helper function
api() {
    curl -s -X "$1" "$API$2" \
        -H "X-API-Key: $API_KEY" \
        -H "Content-Type: application/json" \
        "${@:3}"
}

# 1. Create a sandbox
echo "1. Creating sandbox..."
RESPONSE=$(api POST "/api/v1/sandboxes" -d '{
    "name": "quickstart-demo",
    "templateName": "python-sandbox",
    "autoStart": true
}')
ID=$(echo "$RESPONSE" | jq -r '.id')
echo "   Created: $ID"

# 2. Wait for running state
echo "2. Waiting for sandbox to be ready..."
for i in {1..30}; do
    STATE=$(api GET "/api/v1/sandboxes/$ID" | jq -r '.state')
    if [ "$STATE" = "running" ]; then
        echo "   Ready!"
        break
    fi
    echo "   State: $STATE (attempt $i/30)"
    sleep 2
done

# 3. Execute a command
echo "3. Executing Python code..."
RESULT=$(api POST "/api/v1/sandboxes/$ID/execute" -d '{
    "command": "python -c \"print(sum(range(100)))\""
}')
echo "   Output: $(echo "$RESULT" | jq -r '.stdout')"

# 4. Upload a file
echo "4. Uploading script..."
SCRIPT='
import json
data = {"message": "Hello from Isola!", "numbers": list(range(5))}
print(json.dumps(data, indent=2))
'
echo "$SCRIPT" > /tmp/demo.py
curl -s -X POST "$API/api/v1/sandboxes/$ID/files" \
    -H "X-API-Key: $API_KEY" \
    -F "file=@/tmp/demo.py" \
    -F "path=/workspace/demo.py" > /dev/null
echo "   Uploaded /workspace/demo.py"

# 5. Run the uploaded script
echo "5. Running uploaded script..."
RESULT=$(api POST "/api/v1/sandboxes/$ID/execute" -d '{
    "command": "python /workspace/demo.py"
}')
echo "   Output:"
echo "$RESULT" | jq -r '.stdout' | sed 's/^/   /'

# 6. Cleanup
echo "6. Terminating sandbox..."
api DELETE "/api/v1/sandboxes/$ID" > /dev/null
echo "   Done!"

echo ""
echo "=== Quick Start Complete ==="
