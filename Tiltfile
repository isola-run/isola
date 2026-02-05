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
# - sandbox-sidecar: injected by operator into sandbox pods
# - isola-uploader: used by triggered rootfs snapshot jobs
# - isola-restorer: used by sandbox pods to restore from snapshots
update_settings(suppress_unused_image_warnings=["sandbox-sidecar", "isola-uploader", "isola-restorer"])

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
)

docker_build(
    'isola-restorer',
    context='.',
    dockerfile='cmd/restorer/Dockerfile',
    only=['cmd/restorer/', 'internal/', 'go.mod', 'go.sum'],
)

docker_build(
    'sandbox-sidecar',
    context='.',
    dockerfile='cmd/sandbox-sidecar/Dockerfile',
    only=['cmd/sandbox-sidecar/', 'internal/', 'go.mod', 'go.sum'],
)

helm_resource(
    name='isola',
    chart='charts/isola',
    namespace='isola-system',
    flags=[
        '--create-namespace',
        '-f', 'charts/isola/values-dev.yaml',
    ],
    image_deps=['isola-operator', 'sandbox-sidecar', 'isola-uploader', 'isola-restorer', 'api-gateway'],
    image_keys=[
        ('operator.image.repository', 'operator.image.tag'),
        ('operator.sidecar.image.repository', 'operator.sidecar.image.tag'),
        ('operator.sandboxRuntime.gvisor.rootfssnapshot.uploader.image.repository', 'operator.sandboxRuntime.gvisor.rootfssnapshot.uploader.image.tag'),
        ('operator.sandboxRuntime.gvisor.rootfssnapshot.restorer.image.repository', 'operator.sandboxRuntime.gvisor.rootfssnapshot.restorer.image.tag'),
        ('apiGateway.image.repository', 'apiGateway.image.tag'),
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
