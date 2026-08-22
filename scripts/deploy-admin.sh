#!/usr/bin/env bash
set -Eeuo pipefail

if [[ ${EUID} -ne 0 ]]; then
  echo "deploy-admin.sh must run as root" >&2
  exit 1
fi
if [[ $# -ne 4 ]]; then
  echo "usage: deploy-admin.sh <test|production> <artifact> <sha256> <release-id>" >&2
  exit 1
fi

environment=$1
artifact=$(readlink -f "$2")
expected_sha=$3
release_id=$4
case "${environment}" in test|production) ;; *) echo "invalid environment" >&2; exit 1 ;; esac
[[ ${release_id} =~ ^[A-Za-z0-9._-]+$ ]] || { echo "invalid release id" >&2; exit 1; }
[[ ${expected_sha} =~ ^[a-fA-F0-9]{64}$ ]] || { echo "invalid sha256" >&2; exit 1; }
deployment_recorded=false
trap 'status=$?; if [[ ${deployment_recorded} != true && ${status} -ne 0 && -x /usr/local/sbin/tmk-record-deployment ]]; then /usr/local/sbin/tmk-record-deployment "${environment}" admin-web "${release_id}" failed rollback || true; fi' EXIT
upload_root="/var/lib/tmk-deploy/${environment}"
case "${artifact}" in "${upload_root}"/*) ;; *) echo "artifact must be under ${upload_root}" >&2; exit 1 ;; esac
[[ -f ${artifact} ]] || { echo "admin artifact not found" >&2; exit 1; }
[[ $(sha256sum "${artifact}" | awk '{print $1}') == "${expected_sha,,}" ]] || { echo "admin artifact checksum mismatch" >&2; exit 1; }

root="/opt/tmk-admin/${environment}"
release_dir="${root}/releases/${release_id}"
current_link="${root}/current"
next_link="${root}/.current-${release_id}"
install -d -m 0755 -o root -g root "${root}/releases"
[[ ! -e ${release_dir} ]] || { echo "admin release already exists" >&2; exit 1; }
install -d -m 0755 -o root -g root "${release_dir}"
tar -tzf "${artifact}" | awk '$0 ~ /(^|\/)\.\.(\/|$)/ || $0 ~ /^\// { exit 1 }' || { echo "unsafe admin archive" >&2; exit 1; }
tar -xzf "${artifact}" --no-same-owner --no-same-permissions -C "${release_dir}"
find "${release_dir}" -type d -exec chmod 0755 {} +
find "${release_dir}" -type f -exec chmod 0644 {} +
[[ -s "${release_dir}/index.html" ]] || { echo "admin index.html missing" >&2; exit 1; }
rm -f "${next_link}"
ln -s "${release_dir}" "${next_link}"
mv -Tf "${next_link}" "${current_link}"
rm -f "${artifact}"
/usr/local/sbin/tmk-record-deployment "${environment}" admin-web "${release_id}" success deploy
deployment_recorded=true
echo "deployed admin ${environment} release ${release_id}"
