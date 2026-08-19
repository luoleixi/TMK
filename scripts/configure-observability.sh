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
alertmanager_source="${repo_root}/deploy/prometheus/tmk-alertmanager.yml"
blackbox_source="${repo_root}/deploy/prometheus/blackbox.yml"
vector_source="${repo_root}/deploy/logging/vector.yaml"
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
install -m 0644 -o root -g root "${blackbox_source}" /etc/prometheus/blackbox.yml
promtool check config /etc/prometheus/prometheus.yml
promtool check rules /etc/prometheus/tmk-alerts.yml
if command -v prometheus-alertmanager >/dev/null 2>&1 || command -v alertmanager >/dev/null 2>&1; then
  install -m 0644 -o root -g root "${alertmanager_source}" /etc/prometheus/alertmanager.yml
  systemctl enable prometheus-alertmanager >/dev/null 2>&1 || systemctl enable alertmanager >/dev/null
  systemctl restart prometheus-alertmanager 2>/dev/null || systemctl restart alertmanager
else
  echo "alertmanager is not installed" >&2
  exit 1
fi
if command -v prometheus-blackbox-exporter >/dev/null 2>&1 || command -v blackbox_exporter >/dev/null 2>&1; then
  systemctl enable prometheus-blackbox-exporter >/dev/null 2>&1 || systemctl enable blackbox-exporter >/dev/null
  systemctl restart prometheus-blackbox-exporter 2>/dev/null || systemctl restart blackbox-exporter
else
  echo "blackbox exporter is not installed" >&2
  exit 1
fi
if command -v vector >/dev/null 2>&1; then
  install -d -m 0755 -o root -g root /etc/vector /var/log/tmk
  install -m 0644 -o root -g root "${vector_source}" /etc/vector/vector.yaml
  vector validate /etc/vector/vector.yaml
  systemctl enable vector >/dev/null
  systemctl restart vector
else
  echo "warning: Vector is not installed; centralized local log collection was not started" >&2
fi
systemctl enable prometheus >/dev/null
systemctl restart prometheus
systemctl is-active --quiet prometheus
echo "TMK Prometheus configuration installed"
