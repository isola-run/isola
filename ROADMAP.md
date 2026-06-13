# Isola Roadmap

This is a sketch of where Isola is going. Plans shift as users hit limitations
and contributors pick up work, so nothing here is a firm commitment.

Day-to-day work lives in the
[issues](https://github.com/isola-run/isola/issues). To put something on the
list, open an issue or start a
[discussion](https://github.com/isola-run/isola/discussions).

## Themes

A few areas matter more than any single feature:

- Tightening the sandbox boundary with finer-grained egress controls, runtime
  hardening, and supply-chain integrity. Signed images, SBOM attestations, and a
  signed Helm chart already ship today.
- Getting the REST API and CRDs out of alpha to a stable v1, with a documented
  deprecation policy.
- Making Isola easy to deploy, observe, and run in production, with
  documentation that covers the common cases.
- Broadening SDK coverage, moving to vendor-neutral governance through CNCF, and
  growing the project past a single maintainer.
