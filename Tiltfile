# -*- mode: Python -*-
# Tiltfile for isola local development
# Start with: tilt up

load('ext://helm_resource', 'helm_resource', 'helm_repo')
load('ext://namespace', 'namespace_create')

# ==============================================================================
# Configuration
# ==============================================================================

# Only allow this Tiltfile to run against our local Kind cluster
allow_k8s_contexts('kind-isola-dev')

# Local registry (created by setup.sh)
default_registry('localhost:5001')

# Suppress warning for isola-agent (it's used as sidecar, injected by operator)
update_settings(suppress_unused_image_warnings=["isola-agent"])

# ==============================================================================
# Namespaces
# ==============================================================================

# Namespaces are created by Helm charts:
# - isola-system: created by helm --create-namespace flag
# - isola-sandboxes: created by isola-gw chart template
# - localstack: created by helm --create-namespace flag
# We don't use namespace_create() to avoid label conflicts with Helm

# ==============================================================================
# LocalStack (S3 storage backend)
# ==============================================================================

helm_repo('localstack-repo', 'https://localstack.github.io/helm-charts')
helm_resource(
    'localstack',
    'localstack-repo/localstack',
    namespace='localstack',
    flags=[
        '--create-namespace',
        '--set', 'service.type=ClusterIP',
        '--set', 'startServices=s3',
    ],
    resource_deps=['localstack-repo'],
    labels=['infrastructure'],
)

# Create S3 bucket after LocalStack is ready
local_resource(
    'localstack-bucket',
    cmd='kubectl -n localstack exec deploy/localstack -- awslocal s3api create-bucket --bucket isola-uploads 2>/dev/null || true',
    resource_deps=['localstack'],
    labels=['infrastructure'],
)

# ==============================================================================
# isola-operator
# ==============================================================================

docker_build(
    'isola-operator',
    context='services/isola-operator',
    dockerfile='services/isola-operator/Dockerfile',
    only=[
        'cmd/',
        'internal/',
        'api/',
        'go.mod',
        'go.sum',
    ],
    live_update=[
        sync('services/isola-operator/internal', '/workspace/internal'),
        sync('services/isola-operator/api', '/workspace/api'),
        sync('services/isola-operator/cmd', '/workspace/cmd'),
    ],
)

helm_resource(
    'isola-operator',
    'charts/isola-operator',
    namespace='isola-system',
    flags=[
        '--create-namespace',
        '-f', 'charts/isola-operator/values-dev.yaml',
        '--set', 'image.repository=isola-operator',
        '--set', 'image.tag=latest',
        '--set', 'agentImage=isola-agent:dev',
    ],
    image_deps=['isola-operator', 'isola-agent'],
    image_keys=[
        ('image.repository', 'image.tag'),
        'agentImage',  # Single key format for agent image
    ],
    deps=['charts/isola-operator'],
    labels=['isola'],
)

# ==============================================================================
# isola-agent (sidecar - built but deployed by operator)
# ==============================================================================

docker_build(
    'isola-agent',
    context='services/isola-agent',
    dockerfile='services/isola-agent/Dockerfile',
    only=[
        'cmd/',
        'internal/',
        'go.mod',
        'go.sum',
    ],
    live_update=[
        sync('services/isola-agent/internal', '/workspace/internal'),
        sync('services/isola-agent/cmd', '/workspace/cmd'),
    ],
)

# ==============================================================================
# isola-gw (API Gateway)
# ==============================================================================

docker_build(
    'isola-gw',
    context='services/isola-gw',
    dockerfile='services/isola-gw/Dockerfile',
    only=[
        'cmd/',
        'internal/',
        'go.mod',
        'go.sum',
    ],
    live_update=[
        sync('services/isola-gw/internal', '/workspace/internal'),
        sync('services/isola-gw/cmd', '/workspace/cmd'),
    ],
)

helm_resource(
    'isola-gw',
    'charts/isola-gw',
    namespace='isola-system',
    flags=[
        '--create-namespace',
        '-f', 'charts/isola-gw/values-dev.yaml',
        '--set', 'image.repository=isola-gw',
        '--set', 'image.tag=latest',
    ],
    image_deps=['isola-gw'],
    image_keys=[('image.repository', 'image.tag')],
    resource_deps=['isola-operator', 'localstack-bucket'],
    deps=['charts/isola-gw'],
    labels=['isola'],
)

# ==============================================================================
# Port Forwards & Resources
# ==============================================================================

k8s_resource(
    workload='isola-gw',
    port_forwards=['30080:8080'],
    labels=['isola'],
)

k8s_resource(
    workload='isola-operator',
    labels=['isola'],
)

# ==============================================================================
# E2E Tests (manual trigger)
# ==============================================================================

local_resource(
    'e2e-tests',
    cmd='./scripts/test-e2e.sh --smoke',
    deps=['tests/'],
    auto_init=False,
    trigger_mode=TRIGGER_MODE_MANUAL,
    labels=['tests'],
)

local_resource(
    'e2e-tests-all',
    cmd='./scripts/test-e2e.sh',
    deps=['tests/'],
    auto_init=False,
    trigger_mode=TRIGGER_MODE_MANUAL,
    labels=['tests'],
)
