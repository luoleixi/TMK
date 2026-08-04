#!/usr/bin/env bash
set -Eeuo pipefail

if [[ ${EUID} -ne 0 ]]; then
  echo "configure-test-server.sh must run as root" >&2
  exit 1
fi

legacy_config=${1:-/luoleixi/config.yaml}
api_key=""

if [[ -f ${legacy_config} ]]; then
  api_key=$(awk '
    /^[[:space:]]*api_key:[[:space:]]*/ {
      sub(/^[^:]*:[[:space:]]*/, "")
      gsub(/^[[:space:]"'\'' ]+|[[:space:]"'\'' ]+$/, "")
      if (length($0) > 0) { print; exit }
    }
  ' "${legacy_config}")
fi

if [[ -z ${api_key} ]]; then
  legacy_pid=$(pgrep -f '(^|/)tmk-glance-linux($| )' | head -n 1 || true)
  if [[ -n ${legacy_pid} && -r /proc/${legacy_pid}/environ ]]; then
    api_key=$(tr '\0' '\n' </proc/"${legacy_pid}"/environ | sed -nE 's/^(DASHSCOPE_API_KEY|API_KEY)=//p' | head -n 1)
  fi
fi

if [[ -z ${api_key} ]]; then
  echo "cannot locate the legacy model API key" >&2
  exit 1
fi

db_password=$(openssl rand -hex 24)
mysql --batch <<SQL
CREATE DATABASE IF NOT EXISTS tmk_test CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER IF NOT EXISTS 'tmk_test'@'127.0.0.1' IDENTIFIED BY '${db_password}';
ALTER USER 'tmk_test'@'127.0.0.1' IDENTIFIED BY '${db_password}';
GRANT ALL PRIVILEGES ON tmk_test.* TO 'tmk_test'@'127.0.0.1';
FLUSH PRIVILEGES;
SQL

env_file=/etc/tmk/test/tmk.env
umask 0027
{
  printf 'DASHSCOPE_API_KEY=%s\n' "${api_key}"
  printf 'ASR_PROVIDER=bailian\n'
  printf 'TRANSLATOR_PROVIDER=bailian\n'
  printf 'DB_DRIVER=mysql\n'
  printf 'DB_DSN=tmk_test:%s@tcp(127.0.0.1:3306)/tmk_test?charset=utf8mb4&parseTime=true&loc=Local\n' "${db_password}"
  printf 'SERVER_PORT=:18080\n'
} >"${env_file}"
chown root:tmk-test "${env_file}"
chmod 0640 "${env_file}"

echo "test database and environment configured"
