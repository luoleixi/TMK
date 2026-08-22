#!/usr/bin/env bash
set -Eeuo pipefail
[[ ${EUID} -eq 0 && $# -eq 4 ]] || { echo "usage: deploy-admin-api.sh <test|production> <artifact> <sha256> <release-id>" >&2; exit 1; }
environment=$1; artifact=$(readlink -f "$2"); expected=$3; release=$4
case "$environment" in test|production) ;; *) exit 1 ;; esac
[[ "$artifact" == "/var/lib/tmk-deploy/${environment}"/* && -f "$artifact" ]] || exit 1
[[ "$release" =~ ^[A-Za-z0-9._-]+$ && "$expected" =~ ^[a-fA-F0-9]{64}$ ]] || exit 1
deployment_recorded=false
trap 'status=$?; if [[ ${deployment_recorded} != true && ${status} -ne 0 && -x /usr/local/sbin/tmk-record-deployment ]]; then /usr/local/sbin/tmk-record-deployment "${environment}" admin-api "${release}" failed rollback || true; fi' EXIT
[[ "$(sha256sum "$artifact" | awk '{print $1}')" == "${expected,,}" ]] || exit 1
root="/opt/tmk-admin-api/${environment}"; release_dir="$root/releases/$release"; previous=""; [[ -L "$root/current" ]] && previous=$(readlink -f "$root/current")
install -d -m 0755 "$root/releases"; [[ ! -e "$release_dir" ]] || exit 1; install -d -m 0755 "$release_dir"; gzip -t "$artifact"; gzip -dc "$artifact" > "$release_dir/tmk-admin-api"; chmod 0755 "$release_dir/tmk-admin-api"; ln -s "$release_dir" "$root/.current-$release"; mv -Tf "$root/.current-$release" "$root/current"
systemctl daemon-reload; systemctl enable "tmk-admin-api@${environment}" >/dev/null; systemctl restart "tmk-admin-api@${environment}"
port=18180; [[ "$environment" == production ]] && port=28180
for _ in $(seq 1 20); do
  if curl --fail --silent --max-time 2 "http://127.0.0.1:$port/api/health/live" >/dev/null; then
    rm -f "$artifact"
    /usr/local/sbin/tmk-record-deployment "${environment}" admin-api "${release}" success deploy
    deployment_recorded=true
    exit 0
  fi
  sleep 1
done
if [[ -n "$previous" && -d "$previous" ]]; then ln -s "$previous" "$root/.current-rollback"; mv -Tf "$root/.current-rollback" "$root/current"; systemctl restart "tmk-admin-api@${environment}"; fi
exit 1
