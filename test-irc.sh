docker compose -f docker-compose-irc.yml build --no-cache
docker compose -f docker-compose-irc.yml up -d

function on_exit {
  docker compose logs
  docker compose rm -fsv
}

trap on_exit EXIT

TIMEOUT=60
timeout $TIMEOUT docker compose logs -f | grep -q "answer  | "
timeout $TIMEOUT docker compose logs -f | grep -q "offer   | "