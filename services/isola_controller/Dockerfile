FROM python:3.11-slim

WORKDIR /app

# Copy the entire project (we need all isola modules and services)
COPY . /app

# Install dependencies
RUN pip install --no-cache-dir fastapi uvicorn websockets pydantic

# Expose ports
EXPOSE 3000 8765

# Run the API server which also starts the agent manager
CMD ["python", "-m", "uvicorn", "isola.api.client_gateway:app", "--host", "0.0.0.0", "--port", "3000"]
