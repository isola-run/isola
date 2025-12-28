FROM python:3.11-slim

WORKDIR /app

# Install dependencies
RUN pip install --no-cache-dir websockets pydantic fastapi uvicorn python-multipart httpx aioboto3

# Copy the entire project (we need isola models and services)
COPY . /app

# Expose HTTP port for file uploads
EXPOSE 8080

# Run the agent
CMD ["python", "-m", "services.isola_agent.main"]
