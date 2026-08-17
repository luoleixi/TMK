#!/usr/bin/env bash
set -Eeuo pipefail

if [[ ${EUID} -ne 0 ]]; then
  echo "configure-observability.sh must run as root" >&2
  exit 1
fi

if ! command -v prometheus >/dev/null 2>&1 || ! command -v promtool >/dev/null 2>&1; then
  echo "prometheus and promtool must be installed before configuring observability" >&2
  exit 1
fi

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd "${script_dir}/.." && pwd)
install -d -m 0755 -o root -g root /etc/prometheus
install -m 0644 -o root -g root "${repo_root}/deploy/prometheus/tmk-prometheus.yml" /etc/prometheus/prometheus.yml
install -m 0644 -o root -g root "${repo_root}/deploy/prometheus/tmk-alerts.yml" /etc/prometheus/tmk-alerts.yml
promtool check config /etc/prometheus/prometheus.yml
promtool check rules /etc/prometheus/tmk-alerts.yml
systemctl enable prometheus >/dev/null
systemctl restart prometheus
systemctl is-active --quiet prometheus
echo "TMK Prometheus configuration installed"
