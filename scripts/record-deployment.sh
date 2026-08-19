#!/usr/bin/env bash
set -Eeuo pipefail

if [[ ${EUID} -ne 0 || $# -ne 5 ]]; then
  echo "usage: record-deployment.sh <environment> <service> <release-id> <result> <change-type>" >&2
  exit 1
fi

environment=$1
service=$2
release_id=$3
result=$4
change_type=$5
case "${environment}" in test|production) ;; *) exit 1 ;; esac
for value in "${service}" "${release_id}" "${result}" "${change_type}"; do
  [[ ${value} =~ ^[A-Za-z0-9._-]+$ ]] || exit 1
done

directory="/var/lib/tmk-monitor/${environment}"
file="${directory}/deployments.jsonl"
install -d -m 0750 "${directory}"
printf '{"release_id":"%s","service":"%s","change_type":"%s","version":"%s","commit":"%s","environment":"%s","deployed_at":"%s","result":"%s"}\n' \
  "${release_id}" "${service}" "${change_type}" "${release_id}" "${release_id}" "${environment}" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${result}" >>"${file}"
monitor_user="tmk-monitor-${environment}"
if id "${monitor_user}" >/dev/null 2>&1; then
  chown "${monitor_user}:${monitor_user}" "${directory}" "${file}"
fi
chmod 0750 "${directory}"
chmod 0640 "${file}"
