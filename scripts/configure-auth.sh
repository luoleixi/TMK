#!/usr/bin/env bash
set -Eeuo pipefail

if [[ ${EUID} -ne 0 ]]; then
  echo "configure-auth.sh must run as root" >&2
  exit 1
fi

if [[ $# -ne 2 ]]; then
  echo "usage: configure-auth.sh <test|production> <admin-email>" >&2
  exit 1
fi

environment=$1
admin_email=$2
case "${environment}" in
  test|production) ;;
  *) echo "invalid environment: ${environment}" >&2; exit 1 ;;
esac
if [[ ! ${admin_email} =~ ^[^[:space:]@]+@[^[:space:]@]+\.[^[:space:]@]+$ ]]; then
  echo "invalid admin email" >&2
  exit 1
fi

env_file="/etc/tmk/${environment}/tmk.env"
if [[ ! -f ${env_file} ]]; then
  echo "environment file not found: ${env_file}" >&2
  exit 1
fi

app_group="tmk-${environment}"
initial_password=$(openssl rand -hex 24)
temporary=$(mktemp)
trap 'rm -f "${temporary}"' EXIT

awk '!/^AUTH_BOOTSTRAP_ADMIN_EMAIL=/ && !/^AUTH_BOOTSTRAP_ADMIN_PASSWORD=/' "${env_file}" >"${temporary}"
{
  printf 'AUTH_BOOTSTRAP_ADMIN_EMAIL=%s\n' "${admin_email}"
  printf 'AUTH_BOOTSTRAP_ADMIN_PASSWORD=%s\n' "${initial_password}"
} >>"${temporary}"
install -m 0640 -o root -g "${app_group}" "${temporary}" "${env_file}"

printf 'Bootstrap admin: %s\n' "${admin_email}"
printf 'Initial password: %s\n' "${initial_password}"
echo "Restart the service, change this password at first login, then run clear-bootstrap-auth.sh."
