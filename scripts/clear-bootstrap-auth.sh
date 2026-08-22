#!/usr/bin/env bash
set -Eeuo pipefail

if [[ ${EUID} -ne 0 ]]; then
  echo "clear-bootstrap-auth.sh must run as root" >&2
  exit 1
fi

if [[ $# -ne 1 ]]; then
  echo "usage: clear-bootstrap-auth.sh <test|production>" >&2
  exit 1
fi

environment=$1
case "${environment}" in
  test|production) ;;
  *) echo "invalid environment: ${environment}" >&2; exit 1 ;;
esac

env_file="/etc/tmk/${environment}/tmk.env"
app_group="tmk-${environment}"
temporary=$(mktemp)
trap 'rm -f "${temporary}"' EXIT
awk '!/^AUTH_BOOTSTRAP_ADMIN_EMAIL=/ && !/^AUTH_BOOTSTRAP_ADMIN_PASSWORD=/' "${env_file}" >"${temporary}"
install -m 0640 -o root -g "${app_group}" "${temporary}" "${env_file}"
echo "Bootstrap credentials removed from ${environment}."
