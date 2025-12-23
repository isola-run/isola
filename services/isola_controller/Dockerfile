FROM python:3.11-slim

WORKDIR /app

# Install dependencies
RUN pip install --no-cache-dir fastapi uvicorn websockets pydantic kubernetes httpx python-multipart aioboto3

# Copy the entire project (we need all isola modules and services)
COPY . /app

# Expose ports
EXPOSE 30080 8765

# Run the API server which also starts the agent manager
# todo benl: app is the http server, but we add another server (with lifespan) that starts the WS server
# todo benl: that's a weird hack I am not sure how it would behave and should be revisited / rethink
CMD ["python", "-m", "uvicorn", "services.isola_controller.client_gateway:app", "--host", "0.0.0.0", "--port", "30080"]
