#!/usr/bin/env bash
set -Eeuo pipefail

if [[ ${EUID} -ne 0 ]]; then
  echo "configure-nginx-capacity.sh must run as root" >&2
  exit 1
fi

connections=${1:-4096}
nofile=${2:-65535}
if [[ ! ${connections} =~ ^[0-9]+$ || ! ${nofile} =~ ^[0-9]+$ || ${connections} -lt 1024 || ${nofile} -lt ${connections} ]]; then
  echo "usage: configure-nginx-capacity.sh [worker_connections>=1024] [worker_rlimit_nofile>=worker_connections]" >&2
  exit 2
fi

config=/etc/nginx/nginx.conf
backup="${config}.tmk-backup-$(date +%Y%m%d%H%M%S)"
cp -a "${config}" "${backup}"

if grep -qE '^[[:space:]]*worker_rlimit_nofile[[:space:]]+' "${config}"; then
  sed -i -E "s/^[[:space:]]*worker_rlimit_nofile[[:space:]]+[0-9]+;/worker_rlimit_nofile ${nofile};/" "${config}"
else
  sed -i -E "/^[[:space:]]*worker_processes[[:space:]]+/a worker_rlimit_nofile ${nofile};" "${config}"
fi
sed -i -E "s/^([[:space:]]*worker_connections[[:space:]]+)[0-9]+;/\1${connections};/" "${config}"

if ! nginx -t; then
  cp -a "${backup}" "${config}"
  nginx -t
  echo "invalid Nginx configuration; restored ${backup}" >&2
  exit 1
fi

systemctl reload nginx
echo "Nginx configured: worker_connections=${connections}, worker_rlimit_nofile=${nofile}; backup=${backup}"
