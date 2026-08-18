#!/usr/bin/env bash
set -Eeuo pipefail

if [[ ${EUID} -ne 0 ]]; then
  echo "bootstrap-server.sh must run as root" >&2
  exit 1
fi

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd "${script_dir}/.." && pwd)

install -m 0755 "${repo_root}/scripts/deploy-server.sh" /usr/local/sbin/tmk-deploy
install -m 0755 "${repo_root}/scripts/configure-auth.sh" /usr/local/sbin/tmk-configure-auth
install -m 0755 "${repo_root}/scripts/clear-bootstrap-auth.sh" /usr/local/sbin/tmk-clear-bootstrap-auth
install -m 0755 "${repo_root}/scripts/configure-observability.sh" /usr/local/sbin/tmk-configure-observability
install -m 0755 "${repo_root}/scripts/configure-nginx-capacity.sh" /usr/local/sbin/tmk-configure-nginx-capacity
install -m 0755 "${repo_root}/scripts/deploy-admin.sh" /usr/local/sbin/tmk-deploy-admin
install -m 0755 "${repo_root}/scripts/deploy-monitor.sh" /usr/local/sbin/tmk-deploy-monitor
install -m 0644 "${repo_root}/deploy/systemd/tmk-glance@.service" /etc/systemd/system/tmk-glance@.service
install -m 0644 "${repo_root}/deploy/systemd/tmk-monitor@.service" /etc/systemd/system/tmk-monitor@.service

for environment in test production; do
  app_user="tmk-${environment}"
  deploy_user="tmk-deploy-${environment}"

  if ! id "${app_user}" >/dev/null 2>&1; then
    useradd --system --home-dir "/var/lib/tmk/${environment}" --shell /usr/sbin/nologin "${app_user}"
  fi
  if ! id "${deploy_user}" >/dev/null 2>&1; then
    useradd --create-home --shell /bin/bash "${deploy_user}"
  fi

  install -d -m 0755 -o root -g root "/opt/tmk/${environment}/releases"
  install -d -m 0750 -o "${app_user}" -g "${app_user}" "/var/lib/tmk/${environment}"
  install -d -m 0750 -o "${deploy_user}" -g "${deploy_user}" "/var/lib/tmk-deploy/${environment}"
  install -d -m 0750 -o root -g "${app_user}" "/etc/tmk/${environment}"

  if [[ ! -f /etc/tmk/${environment}/config.yaml ]]; then
    install -m 0640 -o root -g "${app_user}" \
      "${repo_root}/deploy/config/${environment}.yaml" "/etc/tmk/${environment}/config.yaml"
  fi
  if [[ ! -f /etc/tmk/${environment}/tmk.env ]]; then
    install -m 0640 -o root -g "${app_user}" /dev/null "/etc/tmk/${environment}/tmk.env"
  fi

  wrapper="/usr/local/sbin/tmk-deploy-${environment}"
  printf '#!/bin/sh\nexec /usr/local/sbin/tmk-deploy %s "$@"\n' "${environment}" >"${wrapper}"
  chmod 0755 "${wrapper}"

  admin_wrapper="/usr/local/sbin/tmk-deploy-admin-${environment}"
  printf '#!/bin/sh\nexec /usr/local/sbin/tmk-deploy-admin %s "$@"\n' "${environment}" >"${admin_wrapper}"
  chmod 0755 "${admin_wrapper}"

  monitor_wrapper="/usr/local/sbin/tmk-deploy-monitor-${environment}"
  printf '#!/bin/sh\nexec /usr/local/sbin/tmk-deploy-monitor %s "$@"\n' "${environment}" >"${monitor_wrapper}"
  chmod 0755 "${monitor_wrapper}"

  sudoers="/etc/sudoers.d/tmk-deploy-${environment}"
  printf '%s ALL=(root) NOPASSWD: %s *\n' "${deploy_user}" "${wrapper}" >"${sudoers}"
  chmod 0440 "${sudoers}"
  visudo -cf "${sudoers}" >/dev/null
  printf '%s ALL=(root) NOPASSWD: %s *\n' "${deploy_user}" "${admin_wrapper}" >"/etc/sudoers.d/tmk-deploy-admin-${environment}"
  chmod 0440 "/etc/sudoers.d/tmk-deploy-admin-${environment}"
  visudo -cf "/etc/sudoers.d/tmk-deploy-admin-${environment}" >/dev/null
  printf '%s ALL=(root) NOPASSWD: %s *\n' "${deploy_user}" "${monitor_wrapper}" >"/etc/sudoers.d/tmk-deploy-monitor-${environment}"
  chmod 0440 "/etc/sudoers.d/tmk-deploy-monitor-${environment}"
  visudo -cf "/etc/sudoers.d/tmk-deploy-monitor-${environment}" >/dev/null
done

for environment in test production; do
  monitor_user="tmk-monitor-${environment}"
  if ! id "${monitor_user}" >/dev/null 2>&1; then
    useradd --system --home-dir "/var/lib/tmk-monitor/${environment}" --shell /usr/sbin/nologin "${monitor_user}"
  fi
  install -d -m 0750 -o "${monitor_user}" -g "${monitor_user}" "/var/lib/tmk-monitor/${environment}"
  install -d -m 0750 -o root -g "${monitor_user}" "/etc/tmk-monitor"
  if [[ ! -f "/etc/tmk-monitor/${environment}.env" ]]; then
    monitor_port=":19090"
    health_url="http://127.0.0.1:18080/api/health/ready"
    [[ ${environment} == production ]] && monitor_port=":29090" && health_url="http://127.0.0.1:8080/api/health/ready"
    umask 0027
    {
      printf 'MONITOR_PORT=%s\n' "${monitor_port}"
      printf 'MONITOR_ENVIRONMENT=%s\n' "${environment}"
      printf 'PROMETHEUS_URL=http://127.0.0.1:9090\n'
      printf 'ALERTMANAGER_URL=http://127.0.0.1:9093\n'
      printf 'TMK_HEALTH_URL=%s\n' "${health_url}"
    } >"/etc/tmk-monitor/${environment}.env"
    chown root:"${monitor_user}" "/etc/tmk-monitor/${environment}.env"
    chmod 0640 "/etc/tmk-monitor/${environment}.env"
  fi
done

systemctl daemon-reload
if ! command -v ffmpeg >/dev/null 2>&1; then
  echo "warning: ffmpeg is not installed; async evaluation is limited to 16kHz 16-bit mono PCM WAV" >&2
fi
echo "TMK host layout installed; no application service was started"
