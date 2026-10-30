#!/bin/bash

set -Eeuo pipefail

VERBOSE=0
if [ "${1:-}" = "-v" ]; then
    VERBOSE=1
    shift
fi
verbose() {
    if [ "$VERBOSE" -eq 1 ]; then printf "$@" >&2; fi
}
run_setup() {
    if [ "$VERBOSE" -eq 1 ]; then "$@"; else "$@" >/dev/null; fi
}

if [ "$#" -ne 2 ] || [ -z "${1:-}" ] || [ -z "${2:-}" ]
then
    printf "Usage:\n\tsetupVpnServer.sh [-v] <clientPubKey> <serverPubKeyFile>\n" 1>&2
    exit 1
fi

CLIENT_PUB_KEY=$1
SERVER_PUB_KEY_FILE=$2
CLIENT_PUB_IP=${SSH_CLIENT:-}
CLIENT_PUB_IP=${CLIENT_PUB_IP%% *}
if [ -z "$CLIENT_PUB_IP" ]
then
    printf "Cannot find client public ip" 1>&2
    exit 1
fi

SERVER_VPN_IP=10.226.0.1
CLIENT_VPN_IP=10.226.0.2
VPN_PORT=26026
WG0="wg0"
SERVER_PRIV_KEY_FILE="vpn.server.key.private"

trap 'status=$?; printf "VPN server: command failed at line %s (exit %s).\n" "$LINENO" "$status" >&2; exit "$status"' ERR

verbose 'VPN server: checking WireGuard and firewall tools...\n'
if ! command -v wg >/dev/null 2>&1; then
    verbose 'VPN server: installing prerequisites and building WireGuard tools...\n'
    run_setup sudo dnf -y install gcc git make iptables
    if [ ! -d wireguard-tools ]; then
        if [ "$VERBOSE" -eq 1 ]; then
            git clone https://git.zx2c4.com/wireguard-tools
        else
            git clone --quiet https://git.zx2c4.com/wireguard-tools
        fi
    fi
    run_setup make -C wireguard-tools/src -j"$(nproc)"
    run_setup sudo make -C wireguard-tools/src install
elif ! command -v iptables >/dev/null 2>&1; then
    verbose 'VPN server: installing iptables...\n'
    run_setup sudo dnf -y install iptables
fi
verbose 'VPN server: checking keys...\n'
if [ ! -e "$SERVER_PRIV_KEY_FILE" ]
then
    umask 077
    wg genkey > "$SERVER_PRIV_KEY_FILE"
    chmod 400 "$SERVER_PRIV_KEY_FILE"
fi
if [ ! -e "$SERVER_PUB_KEY_FILE" ]
then
    wg pubkey < "$SERVER_PRIV_KEY_FILE" > "$SERVER_PUB_KEY_FILE"
fi
if [ "$(wg pubkey < "$SERVER_PRIV_KEY_FILE")" != "$(cat "$SERVER_PUB_KEY_FILE")" ]; then
    printf 'VPN server: public and private key files do not match; refusing to start.\n' >&2
    exit 1
fi

verbose 'VPN server: configuring WireGuard interface and peer...\n'
server_link_created=0
server_setup_complete=0
cleanup_server() {
    if [ "$server_link_created" -eq 1 ] && [ "$server_setup_complete" -eq 0 ]; then
        verbose 'VPN server: removing incomplete wg0 interface...\n'
        sudo ip link delete "$WG0" || printf 'VPN server: failed to remove incomplete wg0.\n' >&2
    fi
}
trap cleanup_server EXIT
if sudo ip link show "$WG0" >/dev/null 2>&1; then
    # Do not overwrite another application's interface or private key.
    verbose 'VPN server: wg0 already exists; reusing only if its public key matches.\n'
    existing_key=$(sudo wg show "$WG0" public-key)
    expected_key=$(wg pubkey < "$SERVER_PRIV_KEY_FILE")
    if [ "$existing_key" != "$expected_key" ]; then
        printf 'VPN server: existing wg0 is not using the expected server key; refusing to modify it.\n' >&2
        exit 1
    fi
else
    sudo ip link add dev "$WG0" type wireguard
    server_link_created=1
fi
sudo ip addr replace "$SERVER_VPN_IP/24" dev "$WG0"
sudo wg set "$WG0" private-key "./$SERVER_PRIV_KEY_FILE"
sudo wg set "$WG0" listen-port "$VPN_PORT"
sudo wg set "$WG0" peer "$CLIENT_PUB_KEY" allowed-ips "$CLIENT_VPN_IP/32" endpoint "$CLIENT_PUB_IP:$VPN_PORT" persistent-keepalive 25
sudo ip link set "$WG0" up
verbose 'VPN server: enabling IPv4 forwarding and NAT...\n'
ETH0=$(ip -4 route get "$CLIENT_PUB_IP" | awk '{for (i=1; i<=NF; i++) if ($i == "dev") {print $(i+1); exit}}')
if [ -z "$ETH0" ]; then
    printf 'VPN server: could not determine outbound interface for client.\n' >&2
    exit 1
fi
verbose 'VPN server: outbound interface %s, WireGuard UDP/%s.\n' "$ETH0" "$VPN_PORT"
sudo sh -c "echo '1' > /proc/sys/net/ipv4/ip_forward"
sudo iptables -C FORWARD -i "$WG0" -j ACCEPT 2>/dev/null || sudo iptables -A FORWARD -i "$WG0" -j ACCEPT
sudo iptables -t nat -C POSTROUTING -o "$ETH0" -j MASQUERADE 2>/dev/null || sudo iptables -t nat -A POSTROUTING -o "$ETH0" -j MASQUERADE
server_setup_complete=1
verbose 'VPN server: setup complete (client handshake not yet verified).\n'

exit 0
