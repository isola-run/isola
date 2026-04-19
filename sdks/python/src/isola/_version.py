# Single source of truth for the Python SDK version at runtime.
# Kept in sync with /VERSION and charts/isola/Chart.yaml by hack/bump-version.sh
# (and verified by hack/check-versions.sh). Hatchling reads __version__ from
# this file at build time via [tool.hatch.version] path = "src/isola/_version.py".
__version__ = "0.1.0"
