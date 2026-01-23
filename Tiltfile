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
# - isola-agent: used as sidecar, injected by operator
# - isola-uploader: used by triggered snapshot jobs
update_settings(suppress_unused_image_warnings=["isola-agent", "isola-uploader"])

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
        '--set', 'image.repository=isola-operator',
        '--set', 'image.tag=latest',
        '--set', 'agentImage=isola-agent:dev',
        '--set', 'snapshot.uploaderImage=isola-uploader:dev',
    ],
    # image_deps and image-keys instruct tilt on how to patch the helm charts with the newly built images
    # in values.yaml, new isola-operator image should patch image.{repository, tag}
    # while once a new agentImage is built, the agentImage: (string value) needs to be patched
    image_deps=['isola-operator', 'isola-agent', 'isola-uploader'],
    image_keys=[
        ('image.repository', 'image.tag'),
        'agentImage',
        'snapshot.uploaderImage',
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
        'go.mod',
        'go.sum',
    ]
)

# TODO: Add helm_resource for isola-api when chart is created (Phase 2)

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
        'go.mod',
        'go.sum',
    ]
)

# ==============================================================================
# isola-agent (sidecar - built but deployed by operator)
# ==============================================================================

docker_build(
    'isola-agent',
    context='.',
    dockerfile='cmd/agent/Dockerfile',
    only=[
        'cmd/agent/',
        'internal/agent/',
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
