FROM python:3.11-slim

WORKDIR /app

# Copy the entire project
COPY . .

# Install dependencies
RUN pip install --no-cache-dir websockets pydantic

# Run the agent
CMD ["python", "isola/agent/main.py"]

