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
# isola-operator
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

helm_resource(
    name='isola-operator',
    chart='charts/isola-operator',
    namespace='isola-system',
    flags=[
        '--create-namespace',
        '-f', 'charts/isola-operator/values-dev.yaml',
    ],
    # image_deps and image_keys instruct Tilt on how to patch the Helm values with newly built images.
    # Tilt sets repository to the full registry+repo path (e.g., localhost:5001/isola-operator)
    # and tag to the Tilt-generated tag. The helper templates handle empty registry gracefully.
    image_deps=['isola-operator', 'isola-sidecar', 'isola-uploader'],
    image_keys=[
        ('image.repository', 'image.tag'),
        ('sidecar.image.repository', 'sidecar.image.tag'),
        ('snapshot.uploader.image.repository', 'snapshot.uploader.image.tag'),
    ],
    deps=['charts/isola-operator'],
    resource_deps=['localstack'],
    labels=['isola'],
)

# ==============================================================================
# isola-api
# ==============================================================================

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

helm_resource(
    name='isola-api',
    chart='charts/isola-api',
    namespace='isola-system',
    flags=[
        '--create-namespace',
        '-f', 'charts/isola-api/values-dev.yaml',
    ],
    image_deps=['isola-api'],
    image_keys=[('image.repository', 'image.tag')],
    deps=['charts/isola-api'],
    resource_deps=['isola-operator'],
    labels=['isola'],
)

# ==============================================================================
# isola-uploader (snapshot uploader - built but deployed by operator via Jobs)
# ==============================================================================

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

# ==============================================================================
# isola-sidecar (injected by operator into sandbox pods)
# ==============================================================================

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
