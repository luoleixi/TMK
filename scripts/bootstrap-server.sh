#!/usr/bin/env bash
set -Eeuo pipefail
[[ ${EUID} -eq 0 ]] || { echo "bootstrap-server.sh must run as root" >&2; exit 1; }
repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
install -m 0755 "${repo_root}/scripts/deploy-server.sh" /usr/local/sbin/tmk-deploy
install -m 0755 "${repo_root}/scripts/deploy-admin.sh" /usr/local/sbin/tmk-deploy-admin
install -m 0755 "${repo_root}/scripts/deploy-control-api.sh" /usr/local/sbin/tmk-deploy-control-api
install -m 0755 "${repo_root}/scripts/deploy-monitor.sh" /usr/local/sbin/tmk-deploy-monitor
install -m 0755 "${repo_root}/scripts/record-deployment.sh" /usr/local/sbin/tmk-record-deployment
install -m 0644 "${repo_root}/deploy/systemd/tmk-glance@.service" /etc/systemd/system/tmk-glance@.service
install -m 0644 "${repo_root}/deploy/systemd/tmk-control-api.service" /etc/systemd/system/tmk-control-api.service
install -m 0644 "${repo_root}/deploy/systemd/tmk-monitor.service" /etc/systemd/system/tmk-monitor.service

for environment in test production; do
  app_user="tmk-${environment}"; deploy_user="tmk-deploy-${environment}"
  id "${app_user}" >/dev/null 2>&1 || useradd --system --home-dir "/var/lib/tmk/${environment}" --shell /usr/sbin/nologin "${app_user}"
  id "${deploy_user}" >/dev/null 2>&1 || useradd --create-home --shell /bin/bash "${deploy_user}"
  install -d -m 0755 -o root -g root "/opt/tmk/${environment}/releases"
  install -d -m 0750 -o "${app_user}" -g "${app_user}" "/var/lib/tmk/${environment}"
  install -d -m 0750 -o "${deploy_user}" -g "${deploy_user}" "/var/lib/tmk-deploy/${environment}"
  wrapper="/usr/local/sbin/tmk-deploy-${environment}"; printf '#!/bin/sh\nexec /usr/local/sbin/tmk-deploy %s "$@"\n' "${environment}" >"${wrapper}"; chmod 0755 "${wrapper}"
  printf '%s ALL=(root) NOPASSWD: %s *\n' "${deploy_user}" "${wrapper}" >"/etc/sudoers.d/tmk-deploy-${environment}"; chmod 0440 "/etc/sudoers.d/tmk-deploy-${environment}"
  install -d -m 0750 -o root -g "${app_user}" "/etc/tmk/${environment}"
done

id tmk-control-api >/dev/null 2>&1 || useradd --system --home-dir /var/lib/tmk-control-api --shell /usr/sbin/nologin tmk-control-api
id tmk-monitor >/dev/null 2>&1 || useradd --system --home-dir /var/lib/tmk-monitor --shell /usr/sbin/nologin tmk-monitor
install -d -m 0750 -o tmk-control-api -g tmk-control-api /var/lib/tmk-control-api /opt/tmk-control-api/releases
install -d -m 0750 -o tmk-monitor -g tmk-monitor /var/lib/tmk-monitor /opt/tmk-monitor/releases
install -d -m 0750 -o root -g tmk-control-api /etc/tmk-control-api.d
install -d -m 0750 -o root -g tmk-monitor /etc/tmk-monitor.d

if [[ ! -f /etc/tmk-control-api.env ]]; then
  cat >/etc/tmk-control-api.env <<EOF
ADMIN_API_ADDR=:17180
CONTROL_TEST_GLANCE_URL=http://127.0.0.1:18080
CONTROL_PRODUCTION_GLANCE_URL=http://127.0.0.1:8080
ADMIN_API_AUDIT_LOG=/var/lib/tmk-control-api/audit.jsonl
ADMIN_API_SERVICE_ID=tmk-control-api
EOF
  chown root:tmk-control-api /etc/tmk-control-api.env; chmod 0640 /etc/tmk-control-api.env
fi
if ! grep -q '^ADMIN_API_SERVICE_SECRET=' /etc/tmk-control-api.env; then
  control_secret=$(openssl rand -hex 32)
  printf 'ADMIN_API_SERVICE_SECRET=%s\n' "${control_secret}" >>/etc/tmk-control-api.env
  for environment in test production; do
    printf 'ADMIN_API_SERVICE_ID=tmk-control-api\nADMIN_API_SERVICE_SECRET=%s\n' "${control_secret}" >>"/etc/tmk/${environment}/tmk.env"
  done
fi
if [[ ! -f /etc/tmk-monitor.env ]]; then
  cat >/etc/tmk-monitor.env <<EOF
MONITOR_PORT=:17090
MONITOR_TEST_PROMETHEUS_URL=http://127.0.0.1:9090
MONITOR_PRODUCTION_PROMETHEUS_URL=http://127.0.0.1:9090
MONITOR_TEST_ALERTMANAGER_URL=http://127.0.0.1:9093
MONITOR_PRODUCTION_ALERTMANAGER_URL=http://127.0.0.1:9093
MONITOR_TEST_TARGET_HEALTH_URL=http://127.0.0.1:18080/api/health/ready
MONITOR_PRODUCTION_TARGET_HEALTH_URL=http://127.0.0.1:8080/api/health/ready
MONITOR_CONTROL_HEALTH_URL=http://127.0.0.1:17180/api/health/live
MONITOR_BASIC_USER=monitor
MONITOR_BASIC_PASSWORD=$(openssl rand -hex 24)
MONITOR_LOG_PATH=/var/log/tmk/combined.jsonl
EOF
  chown root:tmk-monitor /etc/tmk-monitor.env; chmod 0640 /etc/tmk-monitor.env
fi
systemctl daemon-reload
echo "TMK unified control/monitor deployment layout installed; services were not started"
