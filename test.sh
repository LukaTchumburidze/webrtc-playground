docker compose build
docker compose up -d

function on_exit {
  docker compose logs
  docker compose rm -fsv
}

trap on_exit EXIT

TIMEOUT=30
timeout $TIMEOUT docker compose logs -f | grep -q "answer  | "
timeout $TIMEOUT docker compose logs -f | grep -q "offer   | "