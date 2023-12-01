docker compose -f docker-compose-fs.yml build
docker compose -f docker-compose-fs.yml up -d

function on_exit {
  docker compose logs
  docker compose rm -fsv
}

trap on_exit EXIT

TIMEOUT=300
timeout $TIMEOUT docker compose logs -f | grep -q "answer  | "