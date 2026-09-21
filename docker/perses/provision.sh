#!/bin/sh
set -e

echo "Waiting for Perses API to be ready..."
until percli login http://localhost:8080 --insecure-skip-tls-verify > /dev/null 2>&1; do
  echo "Perses API not ready yet, sleeping..."
  sleep 2
done

echo "Perses API is ready. Starting provisioning..."

# Apply global datasources
echo "Applying global datasources..."
for datasource in /etc/perses/provisioning/datasource/*.yaml; do
  echo "Applying datasource: $datasource"
  percli apply -f "$datasource"
done

# Apply dashboards using native format
echo "Applying dashboards..."
for dashboard in /etc/perses/provisioning/dashboards/*.yaml; do
  if [ -f "$dashboard" ]; then
    echo "Applying dashboard: $dashboard"
    percli apply -f "$dashboard"
  fi
done

echo "Provisioning completed successfully!"
