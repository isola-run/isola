FROM python:3.11-slim

WORKDIR /app

# Copy the entire project
COPY . /app

# Install dependencies
RUN pip install --no-cache-dir websockets pydantic

# Run the agent
CMD ["python", "-m", "isola.agent.main"]

