# Start with: tilt up

load('ext://helm_resource', 'helm_resource', 'helm_repo')

# ==============================================================================
# Configuration
# ==============================================================================

# For safety, only allow Tilt to run with the following cluster
allow_k8s_contexts('kind-isola-dev')

# Local registry (created by hack/setup.sh)
default_registry('localhost:5001')

# Suppress warning for isola-agent (it's used as sidecar, injected by operator)
update_settings(suppress_unused_image_warnings=["isola-agent"])

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
    context='services/isola-operator',
    dockerfile='services/isola-operator/Dockerfile',
    only=[
        'cmd/',
        'internal/',
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
    ],
    # image_deps and image-keys instruct tilt on how to patch the helm charts with the newly built images
    # in values.yaml, new isola-operator image should patch image.{repository, tag}
    # while once a new agentImage is built, the agentImage: (string value) needs to be patched
    image_deps=['isola-operator', 'isola-agent'],
    image_keys=[
        ('image.repository', 'image.tag'),
        'agentImage',
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
    ]
)

local_resource(
    'rebuild-isola-agent',
    cmd='docker build -t localhost:5001/isola-agent:tilt-build services/isola-agent && docker push localhost:5001/isola-agent:tilt-build',
    # don't run this on init, only on manual trigger:
    auto_init=False,
    trigger_mode=TRIGGER_MODE_MANUAL,
    labels=['isola'],
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
    cmd='cd tests && uv run pytest -m smoke',
    deps=['tests/'],
    auto_init=False,
    trigger_mode=TRIGGER_MODE_MANUAL,
    labels=['tests'],
)

local_resource(
    'e2e-tests-all',
    cmd='cd tests && uv run pytest',
    deps=['tests/'],
    auto_init=False,
    trigger_mode=TRIGGER_MODE_MANUAL,
    labels=['tests'],
)
