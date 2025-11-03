#!/bin/bash
# Script to run the Isola sandbox demo

echo "======================================"
echo "   ISOLA SANDBOX DEMO LAUNCHER"
echo "======================================"
echo ""

# Check which tool to use
if command -v uv &> /dev/null; then
    echo "Using uv to run Python scripts..."
    PYTHON_CMD="uv run python3"
elif command -v python3 &> /dev/null; then
    echo "Using python3 directly..."
    PYTHON_CMD="python3"
else
    echo "Error: Python 3 is not installed"
    exit 1
fi

# Function to kill background process on exit
cleanup() {
    echo ""
    echo "Shutting down mock server..."
    if [ ! -z "$SERVER_PID" ]; then
        kill $SERVER_PID 2>/dev/null
    fi
    exit 0
}

# Set trap to cleanup on exit
trap cleanup EXIT INT TERM

# Start the mock server in background
echo "Starting Isola mock server..."
$PYTHON_CMD demo/mock_server.py &
SERVER_PID=$!

# Wait for server to start
echo "Waiting for server to be ready..."
sleep 3

# Check if server is running
if ! kill -0 $SERVER_PID 2>/dev/null; then
    echo "Error: Failed to start mock server"
    echo "Please check if port 3000 is already in use"
    exit 1
fi

echo "Server started successfully (PID: $SERVER_PID)"
echo ""
echo "API Documentation available at: http://localhost:3000/docs"
echo ""
echo "------------------------------------"
echo ""

# Run the demo client
$PYTHON_CMD demo/demo.py

# Script will cleanup automatically via trap
