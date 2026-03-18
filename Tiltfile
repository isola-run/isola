# Start with: tilt up

load('ext://helm_resource', 'helm_resource', 'helm_repo')
load('ext://namespace', 'namespace_create')

# ==============================================================================
# Configuration
# ==============================================================================

# For safety, only allow Tilt to run with the following cluster
allow_k8s_contexts('kind-isola-dev')

# Local registry (created by hack/setup.sh)
default_registry('localhost:5001')
update_settings(suppress_unused_image_warnings=['isola-uploader'])

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
# Isola
# ==============================================================================

docker_build(
    'isola-operator',
    context='.',
    dockerfile='cmd/operator/Dockerfile',
    only=['cmd/operator/', 'internal/', 'api/', 'go.mod', 'go.sum'],
)

docker_build(
    'api-gateway',
    context='.',
    dockerfile='cmd/api-gateway/Dockerfile',
    only=['cmd/api-gateway/', 'internal/', 'api/', 'go.mod', 'go.sum'],
)

docker_build(
    'isola-uploader',
    context='.',
    dockerfile='cmd/uploader/Dockerfile',
    only=['cmd/uploader/', 'internal/', 'go.mod', 'go.sum'],
    match_in_env_vars=True,
)

docker_build(
    'sandbox-sidecar',
    context='.',
    dockerfile='cmd/sandbox-sidecar/Dockerfile',
    only=['cmd/sandbox-sidecar/', 'internal/', 'go.mod', 'go.sum'],
    match_in_env_vars=True,
)

docker_build(
    'snapshot-mounter',
    context='build/snapshot-mounter',
    dockerfile='build/snapshot-mounter/Dockerfile',
)

namespace_create('isola-system')

k8s_yaml(helm(
    'charts/isola',
    name='isola',
    namespace='isola-system',
    values=['charts/isola/values-dev.yaml'],
))

k8s_resource('isola-operator', port_forwards=[port_forward(8082, 8080, name='operator-metrics')], resource_deps=['localstack'], labels=['isola'])
k8s_resource('isola-api-gateway', port_forwards=[port_forward(8080, 8080, name='api-gateway')], resource_deps=['isola-operator'], labels=['isola'])
k8s_resource('isola-snapshot-mounter', resource_deps=['localstack'], labels=['isola'])

# ==============================================================================
# E2E Tests (manual trigger)
# ==============================================================================

local_resource(
    'e2e-tests',
    cmd='cd tests/e2e && uv run --frozen pytest -q',
    deps=['tests/e2e/'],
    auto_init=False,
    trigger_mode=TRIGGER_MODE_MANUAL,
    resource_deps=['isola-api-gateway', 'isola-snapshot-mounter'],
    allow_parallel=True,
    labels=['tests'],
)
