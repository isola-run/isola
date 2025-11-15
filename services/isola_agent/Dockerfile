FROM python:3.11-slim

WORKDIR /app

# Install dependencies
RUN pip install --no-cache-dir websockets pydantic

# Copy the entire project (we need isola models and services)
COPY . /app

# Run the agent
CMD ["python", "-m", "services.isola_agent.main"]

