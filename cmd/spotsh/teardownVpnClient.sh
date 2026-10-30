#!/bin/bash

set -euo pipefail
VERBOSE=0
if [ "${1:-}" = "-v" ]; then
    VERBOSE=1
    shift
fi
if [ "$#" -ne 0 ]; then
    printf 'Usage:\n\tteardownVpnClient.sh [-v]\n' >&2
    exit 1
fi
verbose() {
    if [ "$VERBOSE" -eq 1 ]; then printf "$@" >&2; fi
}
verbose 'VPN client: checking local routing (sudo may request your password)...\n'
sudo -v
has_link=0
if sudo ip link show wg0 >/dev/null 2>&1; then
    has_link=1
    mark=$(sudo wg show wg0 fwmark)
    if [ "$mark" != "0x4d2" ] && [ "$mark" != "1234" ]; then
        printf 'VPN client: wg0 does not have the spotsh fwmark; refusing to modify it or routing rules.\n' >&2
        exit 1
    fi
fi
rules=$(sudo ip -4 rule show)
if printf '%s\n' "$rules" | grep -Eq 'lookup main suppress_prefixlength 0([[:space:]]|$)'; then
    sudo ip rule delete table main suppress_prefixlength 0
fi
if printf '%s\n' "$rules" | grep -Eq 'not .*fwmark 0x4d2(/0x[fF]+)? lookup (2468|0x9a4)([[:space:]]|$)'; then
    sudo ip rule delete not fwmark 1234 table 2468
fi
if [ "$has_link" -eq 1 ]; then
    # Removing the interface also removes its route, without flushing other routes.
    sudo ip link delete wg0
fi
verbose 'VPN client: local routing cleanup complete.\n'
