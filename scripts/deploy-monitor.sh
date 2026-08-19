#!/usr/bin/env bash
set -Eeuo pipefail

if [[ ${EUID} -ne 0 ]]; then
  echo "deploy-monitor.sh must run as root" >&2
  exit 1
fi
if [[ $# -ne 4 ]]; then
  echo "usage: deploy-monitor.sh <test|production> <artifact> <sha256> <release-id>" >&2
  exit 1
fi

environment=$1
artifact=$(readlink -f "$2")
expected_sha=$3
release_id=$4
case "${environment}" in test) health_port=19090 ;; production) health_port=29090 ;; *) echo "invalid environment" >&2; exit 1 ;; esac
[[ ${release_id} =~ ^[A-Za-z0-9._-]+$ ]] || { echo "invalid release id" >&2; exit 1; }
[[ ${expected_sha} =~ ^[a-fA-F0-9]{64}$ ]] || { echo "invalid sha256" >&2; exit 1; }
deployment_recorded=false
trap 'status=$?; if [[ ${deployment_recorded} != true && ${status} -ne 0 && -x /usr/local/sbin/tmk-record-deployment ]]; then /usr/local/sbin/tmk-record-deployment "${environment}" monitor-api "${release_id}" failed rollback || true; fi' EXIT
upload_root="/var/lib/tmk-deploy/${environment}"
case "${artifact}" in "${upload_root}"/*) ;; *) echo "artifact must be under ${upload_root}" >&2; exit 1 ;; esac
[[ -f ${artifact} ]] || { echo "monitor artifact not found" >&2; exit 1; }
[[ $(sha256sum "${artifact}" | awk '{print $1}') == "${expected_sha,,}" ]] || { echo "monitor artifact checksum mismatch" >&2; exit 1; }

root="/opt/tmk-monitor/${environment}"
release_dir="${root}/releases/${release_id}"
current_link="${root}/current"
next_link="${root}/.current-${release_id}"
service="tmk-monitor@${environment}.service"
previous=""
[[ -L ${current_link} ]] && previous=$(readlink -f "${current_link}")
install -d -m 0755 -o root -g root "${root}/releases"
[[ ! -e ${release_dir} ]] || { echo "monitor release already exists" >&2; exit 1; }
install -d -m 0755 -o root -g root "${release_dir}"
if [[ ${artifact} == *.gz ]]; then gzip -t "${artifact}"; gzip -dc "${artifact}" >"${release_dir}/tmk-monitor"; else install -m 0755 -o root -g root "${artifact}" "${release_dir}/tmk-monitor"; fi
chown root:root "${release_dir}/tmk-monitor"; chmod 0755 "${release_dir}/tmk-monitor"
rm -f "${next_link}"; ln -s "${release_dir}" "${next_link}"; mv -Tf "${next_link}" "${current_link}"
systemctl daemon-reload; systemctl enable "${service}" >/dev/null; systemctl restart "${service}"
healthy=false
for _ in $(seq 1 20); do
  if curl --fail --silent --max-time 2 "http://127.0.0.1:${health_port}/api/health/ready" >/dev/null; then healthy=true; break; fi
  sleep 1
done
if [[ ${healthy} != true ]]; then
  echo "monitor health check failed; rolling back" >&2
  journalctl -u "${service}" --since "2 minutes ago" --no-pager -n 50 >&2 || true
  if [[ -n ${previous} && -d ${previous} ]]; then rm -f "${next_link}"; ln -s "${previous}" "${next_link}"; mv -Tf "${next_link}" "${current_link}"; systemctl restart "${service}"; fi
  exit 1
fi
rm -f "${artifact}"
/usr/local/sbin/tmk-record-deployment "${environment}" monitor-api "${release_id}" success deploy
deployment_recorded=true
echo "deployed monitor ${environment} release ${release_id}"
