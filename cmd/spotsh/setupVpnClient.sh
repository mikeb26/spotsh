#!/bin/bash

# https://www.wireguard.com/netns/#routing-all-your-traffic

set -Eeuo pipefail

VERBOSE=0
if [ "${1:-}" = "-v" ]; then
    VERBOSE=1
    shift
fi
verbose() {
    if [ "$VERBOSE" -eq 1 ]; then printf "$@" >&2; fi
}

if [ "$#" -ne 3 ] || [ -z "${1:-}" ] || [ -z "${2:-}" ] || [ -z "${3:-}" ]
then
    printf "Usage:\n\tsetupVpnClient.sh [-v] <serverPubKey> <serverPubIp> <clientPrivKeyFile>\n" 1>&2
    exit 1
fi

SERVER_PUB_KEY=$1
SERVER_PUB_IP=$2
CLIENT_PRIV_KEY_FILE=$3
SERVER_VPN_IP=10.226.0.1
CLIENT_VPN_IP=10.226.0.2
VPN_PORT=26026

verbose 'VPN client: checking existing interface (sudo may request your password)...\n'
sudo -v
if sudo ip link show wg0 >/dev/null 2>&1
then
    printf "wg0 already exists; run 'spotsh vpn stop' before starting a new VPN session\n" 1>&2
    exit 1
fi
if ! command -v ping >/dev/null 2>&1; then
    printf 'VPN client: ping is required for the tunnel reachability check.\n' >&2
    exit 1
fi
rules=$(sudo ip -4 rule show)
if printf '%s\n' "$rules" | grep -Eq 'lookup main suppress_prefixlength 0([[:space:]]|$)|not .*fwmark 0x4d2(/0x[fF]+)? lookup (2468|0x9a4)([[:space:]]|$)'; then
    printf "VPN client: conflicting routing rules already exist; inspect them before retrying or run 'spotsh vpn stop' if they belong to spotsh.\n" >&2
    exit 1
fi

created_link=0
added_route=0
added_vpn_rule=0
added_main_rule=0
cleanup() {
    verbose 'VPN client: setup failed; removing changes made by this attempt...\n'
    if [ "$added_main_rule" -eq 1 ]; then sudo ip rule delete table main suppress_prefixlength 0 || printf "Warning: could not remove main routing rule; run 'spotsh vpn stop'\n" >&2; fi
    if [ "$added_vpn_rule" -eq 1 ]; then sudo ip rule delete not fwmark 1234 table 2468 || printf "Warning: could not remove VPN routing rule; run 'spotsh vpn stop'\n" >&2; fi
    if [ "$added_route" -eq 1 ]; then sudo ip route delete default dev wg0 table 2468 || printf "Warning: could not remove VPN route; run 'spotsh vpn stop'\n" >&2; fi
    if [ "$created_link" -eq 1 ]; then sudo ip link delete wg0 || printf "Warning: could not remove wg0; run 'spotsh vpn stop'\n" >&2; fi
}

trap 'status=$?; printf "VPN client: command failed at line %s (exit %s).\n" "$LINENO" "$status" >&2; exit "$status"' ERR
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'status=$?; if [ "$status" -ne 0 ]; then cleanup; fi' EXIT

verbose 'VPN client: creating wg0 and configuring peer (UDP/%s)...\n' "$VPN_PORT"
sudo ip link add dev wg0 type wireguard
created_link=1
sudo ip addr add "$CLIENT_VPN_IP/24" dev wg0
sudo wg set wg0 private-key "$CLIENT_PRIV_KEY_FILE"
sudo wg set wg0 listen-port "$VPN_PORT"
sudo wg set wg0 peer "$SERVER_PUB_KEY" allowed-ips 0.0.0.0/0 endpoint "$SERVER_PUB_IP:$VPN_PORT" persistent-keepalive 25
sudo wg set wg0 fwmark 1234
sudo ip link set wg0 up

# Send traffic through the tunnel without changing the host's default route.
# A handshake alone does not prove that forwarding or DNS works.
verbose 'VPN client: checking tunnel handshake and server reachability before changing the default route (up to 15 seconds)...\n'
reachable=0
for attempt in {1..15}; do
    if ping -n -c 1 -W 1 -I wg0 "$SERVER_VPN_IP" >/dev/null 2>&1; then
        reachable=1
        break
    fi
done
handshake=$(sudo wg show wg0 latest-handshakes | awk '$1 == key {print $2}' key="$SERVER_PUB_KEY")
if [ "$reachable" -eq 1 ] && [ "${handshake:-0}" = 0 ]; then
    printf 'VPN client: server ping succeeded, but WireGuard reported no handshake; refusing to change routing.\n' >&2
    exit 1
fi
if [ "$reachable" -ne 1 ]; then
    if [ "$VERBOSE" -eq 1 ]; then
        transfer=$(sudo wg show wg0 transfer | awk '$1 == key {rx += $2; tx += $3} END {printf "%.0f received, %.0f sent", rx, tx}' key="$SERVER_PUB_KEY")
        verbose 'VPN client: WireGuard transfer: %s bytes.\n' "$transfer"
    fi
    if [ "${handshake:-0}" != 0 ]; then
        printf 'VPN client: handshake observed, but server tunnel address did not respond; check server firewall and wg0.\n' >&2
    else
        printf 'VPN client: no handshake; check UDP/26026 ingress, client/server keys, NAT and the server WireGuard interface.\n' >&2
    fi
    exit 1
fi
verbose 'VPN client: tunnel server responded; installing full-tunnel IPv4 routing...\n'
sudo ip route add default dev wg0 table 2468
added_route=1
sudo ip rule add not fwmark 1234 table 2468
added_vpn_rule=1
sudo ip rule add table main suppress_prefixlength 0
added_main_rule=1

verbose 'VPN client: routing configured. A handshake does not verify internet forwarding or DNS.\n'
trap - ERR INT TERM EXIT

exit 0
