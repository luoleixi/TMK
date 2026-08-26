#!/usr/bin/env bash
set -Eeuo pipefail

if [[ ${EUID} -ne 0 ]]; then
  echo "deploy-server.sh must run as root" >&2
  exit 1
fi

if [[ $# -ne 4 ]]; then
  echo "usage: deploy-server.sh <test|production> <artifact> <sha256> <release-id>" >&2
  exit 1
fi

environment=$1
artifact=$2
expected_sha=$3
release_id=$4

case "${environment}" in
  test)
    health_port=18080
    ;;
  production)
    health_port=8080
    ;;
  *)
    echo "invalid environment: ${environment}" >&2
    exit 1
    ;;
esac

if [[ ! ${release_id} =~ ^[A-Za-z0-9._-]+$ ]]; then
  echo "invalid release id" >&2
  exit 1
fi
if [[ ! ${expected_sha} =~ ^[a-fA-F0-9]{64}$ ]]; then
  echo "invalid sha256" >&2
  exit 1
fi
deployment_recorded=false
trap 'status=$?; if [[ ${deployment_recorded} != true && ${status} -ne 0 && -x /usr/local/sbin/tmk-record-deployment ]]; then /usr/local/sbin/tmk-record-deployment "${environment}" glance "${release_id}" failed rollback || true; fi' EXIT

upload_root="/var/lib/tmk-deploy/${environment}"
artifact=$(readlink -f "${artifact}")
case "${artifact}" in
  "${upload_root}"/*) ;;
  *)
    echo "artifact must be uploaded below ${upload_root}" >&2
    exit 1
    ;;
esac
if [[ ! -f ${artifact} ]]; then
  echo "artifact not found: ${artifact}" >&2
  exit 1
fi

actual_sha=$(sha256sum "${artifact}" | awk '{print $1}')
if [[ ${actual_sha} != "${expected_sha,,}" ]]; then
  echo "artifact checksum mismatch" >&2
  exit 1
fi

app_root="/opt/tmk/${environment}"
release_dir="${app_root}/releases/${release_id}"
current_link="${app_root}/current"
next_link="${app_root}/.current-${release_id}"
service="tmk-glance@${environment}.service"
previous=""

if [[ -L ${current_link} ]]; then
  previous=$(readlink -f "${current_link}")
fi

install -d -m 0755 -o root -g root "${app_root}/releases"
if [[ -e ${release_dir} ]]; then
  echo "release already exists: ${release_id}" >&2
  exit 1
fi
install -d -m 0755 -o root -g root "${release_dir}"
case "${artifact}" in
  *.gz)
    gzip -t "${artifact}"
    gzip -dc "${artifact}" >"${release_dir}/tmk-glance"
    chown root:root "${release_dir}/tmk-glance"
    chmod 0755 "${release_dir}/tmk-glance"
    ;;
  *)
    install -m 0755 -o root -g root "${artifact}" "${release_dir}/tmk-glance"
    ;;
esac

rm -f "${next_link}"
ln -s "${release_dir}" "${next_link}"
mv -Tf "${next_link}" "${current_link}"

systemctl daemon-reload
systemctl enable "${service}" >/dev/null
systemctl restart "${service}"

healthy=false
for _ in $(seq 1 30); do
  if curl --fail --silent --max-time 2 "http://127.0.0.1:${health_port}/api/health/ready" >/dev/null; then
    healthy=true
    break
  fi
  sleep 1
done

if [[ ${healthy} != true ]]; then
  echo "health check failed; rolling back ${environment}" >&2
  systemctl status "${service}" --no-pager --lines=20 >&2 || true
  journalctl -u "${service}" --since "2 minutes ago" --no-pager -n 50 >&2 || true
  if [[ -n ${previous} && -d ${previous} ]]; then
    rm -f "${next_link}"
    ln -s "${previous}" "${next_link}"
    mv -Tf "${next_link}" "${current_link}"
    systemctl restart "${service}"
  else
    rm -f "${current_link}"
    systemctl disable --now "${service}" >/dev/null 2>&1 || true
  fi
  exit 1
fi

rm -f "${artifact}"
if [[ -x /usr/local/sbin/tmk-record-deployment ]]; then
  /usr/local/sbin/tmk-record-deployment "${environment}" glance "${release_id}" success deploy ||
    printf 'warning: deployment succeeded but deployment record failed\n' >&2
else
  printf 'warning: deployment succeeded but tmk-record-deployment is unavailable\n' >&2
fi
deployment_recorded=true
echo "deployed ${environment} release ${release_id}"
