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
config_source="${repo_root}/deploy/prometheus/tmk-prometheus.yml"
alerts_source="${repo_root}/deploy/prometheus/tmk-alerts.yml"
if [[ ! -f ${config_source} && -f /etc/prometheus/tmk-prometheus.yml ]]; then
  config_source=/etc/prometheus/tmk-prometheus.yml
  alerts_source=/etc/prometheus/tmk-alerts.yml
fi
if [[ ! -f ${config_source} || ! -f ${alerts_source} ]]; then
  echo "TMK Prometheus templates not found" >&2
  exit 1
fi
install -d -m 0755 -o root -g root /etc/prometheus
if [[ ${config_source} != /etc/prometheus/prometheus.yml ]]; then
  install -m 0644 -o root -g root "${config_source}" /etc/prometheus/prometheus.yml
fi
if [[ ${alerts_source} != /etc/prometheus/tmk-alerts.yml ]]; then
  install -m 0644 -o root -g root "${alerts_source}" /etc/prometheus/tmk-alerts.yml
fi
promtool check config /etc/prometheus/prometheus.yml
promtool check rules /etc/prometheus/tmk-alerts.yml
systemctl enable prometheus >/dev/null
systemctl restart prometheus
systemctl is-active --quiet prometheus
echo "TMK Prometheus configuration installed"
