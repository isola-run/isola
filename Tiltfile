# Start with: tilt up

load('ext://helm_resource', 'helm_resource', 'helm_repo')

# ==============================================================================
# Configuration
# ==============================================================================

# For safety, only allow Tilt to run with the following cluster
allow_k8s_contexts('kind-isola-dev')

# Local registry (created by hack/setup.sh)
default_registry('localhost:5001')

# Suppress warning for images that are built but deployed indirectly
# - isola-sidecar: injected by operator into sandbox pods
# - isola-uploader: used by triggered snapshot jobs
update_settings(suppress_unused_image_warnings=["isola-sidecar", "isola-uploader"])

# ==============================================================================
# LocalStack (S3 storage backend)
# ==============================================================================

helm_repo(name='localstack-repo', url='https://localstack.github.io/helm-charts')

helm_resource(
    name='localstack',
    chart='localstack-repo/localstack',
    namespace='localstack',
    flags=[
        '--create-namespace',
        '-f', 'charts/localstack-values.yaml',
    ],
    resource_deps=['localstack-repo'],
    labels=['infrastructure'],
)

# ==============================================================================
# Build images
# ==============================================================================

docker_build(
    'isola-operator',
    context='.',
    dockerfile='cmd/operator/Dockerfile',
    only=[
        'cmd/operator/',
        'internal/operator/',
        'internal/snapshot/',
        'api/',
        'go.mod',
        'go.sum',
    ]
)

docker_build(
    'isola-api',
    context='.',
    dockerfile='cmd/isola-api/Dockerfile',
    only=[
        'api/',
        'cmd/isola-api/',
        'internal/api/',
        'internal/logging/',
        'go.mod',
        'go.sum',
    ]
)

docker_build(
    'isola-uploader',
    context='.',
    dockerfile='cmd/uploader/Dockerfile',
    only=[
        'cmd/uploader/',
        'internal/snapshot/',
        'internal/logging/',
        'go.mod',
        'go.sum',
    ]
)

docker_build(
    'isola-sidecar',
    context='.',
    dockerfile='cmd/isola-sidecar/Dockerfile',
    only=[
        'cmd/isola-sidecar/',
        'internal/sidecar/',
        'internal/logging/',
        'go.mod',
        'go.sum',
    ]
)

# ==============================================================================
# isola (unified chart - operator + api)
# ==============================================================================

# Render helm chart - lets Tilt discover individual resources for granular visibility
# Chart creates its own namespaces (isola-system, isola-sandboxes)
watch_file('charts/isola')
k8s_yaml(helm(
    'charts/isola',
    name='isola',
    namespace='isola-system',
    values=['charts/isola/values-dev.yaml'],
    set=[
        # Clear registry so Tilt's default_registry applies to built images
        'operator.image.registry=',
        'operator.image.repository=isola-operator',
        'operator.sidecar.image.registry=',
        'operator.sidecar.image.repository=isola-sidecar',
        'operator.snapshot.uploader.image.registry=',
        'operator.snapshot.uploader.image.repository=isola-uploader',
        'api.image.registry=',
        'api.image.repository=isola-api',
    ],
))

# Configure individual resources for granular visibility and control
k8s_resource(
    'isola-operator',
    labels=['isola'],
    resource_deps=['localstack'],
)

k8s_resource(
    'isola-api',
    port_forwards='8080:8080',
    labels=['isola'],
    resource_deps=['localstack'],
)

# ==============================================================================
# E2E Tests (manual trigger)
# ==============================================================================

local_resource(
    'e2e-tests',
    cmd='cd tests/e2e && uv run pytest -m smoke',
    deps=['tests/e2e/'],
    auto_init=False,
    trigger_mode=TRIGGER_MODE_MANUAL,
    labels=['tests'],
)

local_resource(
    'e2e-tests-all',
    cmd='cd tests/e2e && uv run pytest',
    deps=['tests/e2e/'],
    auto_init=False,
    trigger_mode=TRIGGER_MODE_MANUAL,
    labels=['tests'],
)
