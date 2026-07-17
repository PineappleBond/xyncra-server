#!/bin/sh
# chaos-loop.sh — Main entrypoint for chaos-agent container.
# Runs a loop that checks for /tmp/chaos-active flag file.
# When active, performs randomized rolling restarts of server nodes.
#
# Control:
#   touch /tmp/chaos-active   → start chaos
#   rm /tmp/chaos-active      → stop chaos (after current restart completes)

set -e

FLAG_FILE="/tmp/chaos-active"
COUNTER_FILE="/tmp/chaos-counter"
LOG_FILE="/tmp/chaos.log"

# Node container names (override via environment)
NODE_A="${NODE_A:-xyncra-server-xyncra-node-a-1}"
NODE_B="${NODE_B:-xyncra-server-xyncra-node-b-1}"
NODE_C="${NODE_C:-xyncra-server-xyncra-node-c-1}"

# Random interval range (seconds)
MIN_INTERVAL="${MIN_INTERVAL:-10}"
MAX_INTERVAL="${MAX_INTERVAL:-40}"

# Initialize counter
echo "0" > "$COUNTER_FILE"

log() {
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*" | tee -a "$LOG_FILE"
}

random_interval() {
  # shellcheck disable=SC2004
  echo $(( RANDOM % (MAX_INTERVAL - MIN_INTERVAL + 1) + MIN_INTERVAL ))
}

shuffle_nodes() {
  # Randomly shuffle the 3 node names
  nodes="$NODE_A $NODE_B $NODE_C"
  echo "$nodes" | tr ' ' '\n' | sort -R | tr '\n' ' '
}

restart_node() {
  local node="$1"
  log "Restarting $node ..."
  if docker restart "$node" 2>>"$LOG_FILE"; then
    log "  → $node restart issued"
  else
    log "  → WARNING: $node restart failed (may already be restarting)"
  fi
}

log "chaos-agent started. Waiting for trigger..."
log "  Nodes: $NODE_A, $NODE_B, $NODE_C"
log "  Interval: ${MIN_INTERVAL}-${MAX_INTERVAL}s (random)"

while true; do
  if [ -f "$FLAG_FILE" ]; then
    # Chaos is active — perform one round of rolling restarts
    round=$(cat "$COUNTER_FILE")
    round=$((round + 1))
    echo "$round" > "$COUNTER_FILE"

    log "=== Round $round ==="

    # Shuffle node order for this round
    shuffled=$(shuffle_nodes)
    log "  Order: $shuffled"

    for node in $shuffled; do
      interval=$(random_interval)
      restart_node "$node"
      log "  Sleeping ${interval}s before next restart..."
      sleep "$interval"
    done

    log "=== Round $round complete ==="

    # Brief pause between rounds (allows tester to observe)
    sleep 10
  else
    # Chaos is inactive — wait and check again
    sleep 5
  fi
done
