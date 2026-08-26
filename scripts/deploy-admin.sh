#!/usr/bin/env bash
set -Eeuo pipefail
[[ ${EUID} -eq 0 ]] || { echo "deploy-admin.sh must run as root" >&2; exit 1; }
[[ $# -eq 3 ]] || { echo "usage: deploy-admin.sh <artifact> <sha256> <release-id>" >&2; exit 1; }
artifact=$(readlink -f "$1"); expected_sha=$2; release_id=$3
[[ ${release_id} =~ ^[A-Za-z0-9._-]+$ ]] || { echo "invalid release id" >&2; exit 1; }
[[ ${expected_sha} =~ ^[a-fA-F0-9]{64}$ ]] || { echo "invalid sha256" >&2; exit 1; }
[[ -f ${artifact} && $(sha256sum "${artifact}" | awk '{print $1}') == "${expected_sha,,}" ]] || { echo "admin artifact checksum mismatch" >&2; exit 1; }
case "${artifact}" in /var/lib/tmk-deploy/test/*|/var/lib/tmk-deploy/production/*) ;; *) echo "artifact must be in deployment staging" >&2; exit 1 ;; esac
root=/opt/tmk-admin; release_dir="${root}/releases/${release_id}"; next_link="${root}/.current-${release_id}"
install -d -m 0755 -o root -g root "${root}/releases"; [[ ! -e ${release_dir} ]] || { echo "release already exists" >&2; exit 1; }; install -d -m 0755 -o root -g root "${release_dir}"
tar -tzf "${artifact}" | awk '$0 ~ /(^|\/)\.\.(\/|$)/ || $0 ~ /^\// { exit 1 }' || { echo "unsafe admin archive" >&2; exit 1; }; tar -xzf "${artifact}" --no-same-owner --no-same-permissions -C "${release_dir}"
find "${release_dir}" -type d -exec chmod 0755 {} +; find "${release_dir}" -type f -exec chmod 0644 {} +; [[ -s "${release_dir}/index.html" ]] || { echo "admin index.html missing" >&2; exit 1; }
rm -f "${next_link}"; ln -s "${release_dir}" "${next_link}"; mv -Tf "${next_link}" "${root}/current"; rm -f "${artifact}"; echo "deployed admin ${release_id}"
