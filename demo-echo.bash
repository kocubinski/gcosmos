#!/bin/bash
set -eu

print_config() {
  local n_vals="${1:?number of validators required}"
  local prog template
  prog=$(cat <<EOF
  BEGIN {
    printf "{\"ValidatorPubKeys\": ["
    for (i = 0; i < cfg_vals; i++) {
      if (i > 0) { printf ", " }
      printf "\"%%s\""
    }
    print "], \"RemoteAddrs\": [\"/ip4/127.0.0.1/tcp/9999/p2p/%s\"]}"
  }
EOF
)
  template="$(awk -v cfg_vals="${n_vals}" "$prog")"
  readonly n_vals prog template

  local printf_args=()
  local i
  for (( i=0; i < "$n_vals"; i++)); do
    printf_args+=(
      "$(./bin/gordian-echo validator-pubkey "val-${i}")"
    )
  done

  printf_args+=( "$(./bin/gordian-echo libp2p-id seed)" )

  printf "$template" "${printf_args[@]}"
}

demo () {
  _start () {
    local network_size
    readonly network_size="${1:?network size required}"

    local running_vals
    readonly running_vals="${2:-$1}"

    >&2 echo 'Compiling...'
    go build -o ./bin/gordian-echo ./cmd/gordian-echo

    ./bin/gordian-echo run-p2p-relayer seed > ./bin/seed.log 2>&1 &

    >&2 echo 'Generating config...'
    print_config "$network_size" > ./bin/config.json

    >&2 echo 'Starting validators...'
    local i
    for (( i=0; i < "$running_vals"; i++)); do
      ./bin/gordian-echo run-echo-validator "val-${i}" ./bin/config.json -l "/ip4/127.0.0.1/tcp/0" > "./bin/val-${i}.log" 2>&1 &
    done

    >&2 printf 'Network started; logs available at: '
    for (( i=0; i < "$running_vals"; i++)); do
      >&2 printf "./bin/val-${i}.log "
    done
    >&2 echo
    >&2 echo 'Watch logs with: tail -f ./bin/val-*.log'
    >&2 echo
    >&2 echo "Stop the network with: $0 stop"
  }

  stop () {
    # Send interrupt signal right away, which should allow a clean shutdown.
    pkill -INT -f gordian-echo
    # Now delay an eighth of a second to allow a fast shutdown.
    # We can't rely on bash or /bin/sleep having sub-second sleep,
    # but the system very likely has perl or python.
    (perl -MTime::HiRes=usleep -e 'usleep(125000)' || python3 -c 'import time; time.sleep(0.125)' || true) >/dev/null 2>&1
    if pgrep -q -f gordian-echo; then
      >&2 echo 'Some processes not shut down immediately; waiting a moment before forcing stack dump.'
      sleep 1 # Whole-second sleep is no problem.
      pkill -QUIT gordian-echo
    fi
  }

  clean () {
    rm -rf bin
  }

  commands () {
    >&2 cat <<-EOF
demo commands:
  start NETWORK_SIZE [RUNNING_VALS] -> start the demo network
  startr NETWORK_SIZE [RUNNING_VALS] -> start the demo network, with the data race detector enabled
  stop -> stop the demo network
  clean -> delete the demo folder
EOF
  }

  local cmd="${1:-}"
  shift || true # If no args, shift would cause the script to fail due to set -e at the top.

  if [ "$cmd" == startr ]; then
    # Data race detection enabled -- just set go environment variables,
    # which will propagate to go build.
    cmd=start
    export GOFLAGS=-race GODEBUG=halt_on_error=1
    >&2 echo 'Data race detection enabled'
  fi

  case "$cmd" in
    start)
      if [ $# -ne 1 ] && [ $# -ne 2 ]; then
        >&2 echo "USAGE: $0 NETWORK_SIZE [RUNNING_VALS]

The NETWORK_SIZE argument determines the number of validators in the network.

RUNNING_VALS defaults to NETWORK_SIZE;
otherwise, if provided, it determines the nuber of validators to run locally.
RUNNING_VALS may be set less than or greater than NETWORK_SIZE."
        exit 1
      fi
      _start "$@"
      ;;
    stop)
      stop
      ;;
    clean)
      clean
      ;;
    *)
      commands
      ;;
  esac
}

demo "$@"
