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
# isola-gw (API Gateway)
# ==============================================================================

docker_build(
    'isola-gw',
    context='.',
    dockerfile='cmd/gateway/Dockerfile',
    only=[
        'cmd/gateway/',
        'internal/gateway/',
        'go.mod',
        'go.sum',
    ]
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
    resource_deps=['isola-operator', 'localstack'],
    deps=['charts/isola-gw'],
    labels=['isola'],
)

# ==============================================================================
# Port Forward
# ==============================================================================

k8s_resource(
    workload='isola-gw',
    port_forwards=['30080:8080'],
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
