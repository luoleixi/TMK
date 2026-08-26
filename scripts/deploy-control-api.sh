#!/usr/bin/env bash
set -Eeuo pipefail
[[ ${EUID} -eq 0 ]] || { echo "deploy-control-api.sh must run as root" >&2; exit 1; }
[[ $# -eq 3 ]] || { echo "usage: deploy-control-api.sh <artifact> <sha256> <release-id>" >&2; exit 1; }
artifact=$(readlink -f "$1"); expected_sha=$2; release_id=$3
[[ ${release_id} =~ ^[A-Za-z0-9._-]+$ && ${expected_sha} =~ ^[a-fA-F0-9]{64}$ ]] || exit 1
[[ -f ${artifact} && $(sha256sum "${artifact}" | awk '{print $1}') == "${expected_sha,,}" ]] || { echo "control api artifact checksum mismatch" >&2; exit 1; }
case "${artifact}" in /var/lib/tmk-deploy/test/*|/var/lib/tmk-deploy/production/*) ;; *) echo "artifact must be in deployment staging" >&2; exit 1 ;; esac
root=/opt/tmk-control-api; release_dir="${root}/releases/${release_id}"; next_link="${root}/.current-${release_id}"
install -d -m 0755 -o root -g root "${root}/releases"; [[ ! -e ${release_dir} ]] || exit 1; install -d -m 0755 -o root -g root "${release_dir}"
if [[ ${artifact} == *.gz ]]; then gzip -t "${artifact}"; gzip -dc "${artifact}" >"${release_dir}/tmk-control-api"; else install -m 0755 "${artifact}" "${release_dir}/tmk-control-api"; fi
chmod 0755 "${release_dir}/tmk-control-api"; rm -f "${next_link}"; ln -s "${release_dir}" "${next_link}"; mv -Tf "${next_link}" "${root}/current"; systemctl daemon-reload; systemctl enable --now tmk-control-api.service; systemctl restart tmk-control-api.service
for _ in $(seq 1 20); do curl --fail --silent --max-time 2 http://127.0.0.1:17180/api/health/live >/dev/null && rm -f "${artifact}" && echo "deployed control api ${release_id}" && exit 0; sleep 1; done
echo "control api health check failed" >&2; exit 1
