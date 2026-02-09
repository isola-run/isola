# internal/operator/

Kubernetes operator implementation. All reconciler logic lives in `controller/`.

## Testing

```bash
make test-operator                   # Run all operator tests
make test-operator FOCUS="Reconcile" # Run focused tests by Ginkgo pattern
```

Operator tests use envtest (simulated K8s API) with Ginkgo/Gomega.
