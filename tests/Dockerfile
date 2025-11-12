FROM python:3.11-slim

WORKDIR /tests

# Install curl for manual testing
RUN apt-get update && apt-get install -y curl && rm -rf /var/lib/apt/lists/*

# Install test dependencies
COPY tests/requirements-test.txt .
RUN pip install --no-cache-dir -r requirements-test.txt

# Copy test files
COPY tests/ /tests/
COPY pyproject.toml /tests/

# Set environment for tests to use internal service names
ENV TEST_BASE_URL=http://isola-controller:30080
ENV TEST_API_KEY=iso_sk_demo

# Keep container running
CMD ["tail", "-f", "/dev/null"]
