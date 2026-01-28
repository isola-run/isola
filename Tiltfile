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

helm_resource(
    name='isola',
    chart='charts/isola',
    namespace='isola-system',
    flags=[
        '--create-namespace',
        '-f', 'charts/isola/values-dev.yaml',
    ],
    # image_deps and image_keys instruct Tilt on how to patch the Helm values with newly built images.
    # Tilt sets repository to the full registry+repo path (e.g., localhost:5001/isola-operator)
    # and tag to the Tilt-generated tag. The helper templates handle empty registry gracefully.
    image_deps=['isola-operator', 'isola-sidecar', 'isola-uploader', 'isola-api'],
    image_keys=[
        ('operator.image.repository', 'operator.image.tag'),
        ('operator.sidecar.image.repository', 'operator.sidecar.image.tag'),
        ('operator.snapshot.uploader.image.repository', 'operator.snapshot.uploader.image.tag'),
        ('api.image.repository', 'api.image.tag'),
    ],
    deps=['charts/isola'],
    resource_deps=['localstack'],
    labels=['isola'],
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
