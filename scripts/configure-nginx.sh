#!/usr/bin/env bash
set -Eeuo pipefail

if [[ ${EUID} -ne 0 ]]; then
  echo "configure-nginx.sh must run as root" >&2
  exit 1
fi

if [[ $# -ne 1 ]]; then
  echo "usage: configure-nginx.sh <nginx-site-file>" >&2
  exit 1
fi

site_file=$(readlink -f "$1")
if [[ ! -f ${site_file} ]]; then
  echo "nginx site file not found: $1" >&2
  exit 1
fi

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd "${script_dir}/.." && pwd)
snippet=/etc/nginx/snippets/tmk-locations.conf
include_line='    include /etc/nginx/snippets/tmk-locations.conf;'

install -m 0644 "${repo_root}/deploy/nginx/tmk-locations.conf" "${snippet}"

backup_root=/var/backups/tmk/nginx
install -d -m 0700 -o root -g root "${backup_root}"
backup="${backup_root}/$(basename "${site_file}").$(date +%Y%m%d%H%M%S)"
cp -a "${site_file}" "${backup}"

if ! grep -Fq "${include_line}" "${site_file}"; then
  temporary=$(mktemp)
  awk -v include_line="${include_line}" '
    !inserted && /^[[:space:]]*location \/ \{/ {
      print include_line
      inserted = 1
    }
    { print }
    END { if (!inserted) exit 42 }
  ' "${site_file}" >"${temporary}" || {
    rm -f "${temporary}"
    echo "could not find the root location in ${site_file}" >&2
    exit 1
  }
  cat "${temporary}" >"${site_file}"
  rm -f "${temporary}"
fi

if ! nginx -t; then
  cp -a "${backup}" "${site_file}"
  nginx -t
  echo "nginx validation failed; restored ${backup}" >&2
  exit 1
fi

systemctl reload nginx
echo "TMK nginx routes installed"
