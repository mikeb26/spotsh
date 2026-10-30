#!/bin/bash

# https://www.wireguard.com/netns/#routing-all-your-traffic

set -eu

SERVER_PUB_KEY=$1
SERVER_PUB_IP=$2
CLIENT_PRIV_KEY_FILE=$3

if [ "$SERVER_PUB_KEY" == "" ] || [ "$SERVER_PUB_IP" == "" ] || [ "$CLIENT_PRIV_KEY_FILE" == "" ] || [ "$4" != "" ]
then
    printf "Usage:\n\tsetupVpnClient.sh <serverPubKey> <serverPubIp> <clientPrivKeyFile>\n" 1>&2
    exit 1
fi

SERVER_VPN_IP=10.226.0.1
CLIENT_VPN_IP=10.226.0.2
VPN_PORT=26026

if sudo ip link show wg0 >/dev/null 2>&1
then
    printf "wg0 already exists; run 'spotsh vpn stop' before starting a new VPN session\n" 1>&2
    exit 1
fi

cleanup() {
    sudo ip rule delete table main suppress_prefixlength 0 2>/dev/null || true
    sudo ip rule delete not fwmark 1234 table 2468 2>/dev/null || true
    sudo ip route flush table 2468 2>/dev/null || true
    sudo ip link delete wg0 2>/dev/null || true
}

trap cleanup ERR INT TERM

sudo ip link add dev wg0 type wireguard
sudo ip addr add $CLIENT_VPN_IP/24 dev wg0
sudo wg set wg0 private-key $CLIENT_PRIV_KEY_FILE
sudo wg set wg0 listen-port $VPN_PORT
sudo wg set wg0 peer $SERVER_PUB_KEY allowed-ips 0.0.0.0/0 endpoint $SERVER_PUB_IP:$VPN_PORT persistent-keepalive 25
sudo wg set wg0 fwmark 1234
sudo ip link set wg0 up
sudo ip route add default dev wg0 table 2468
sudo ip rule add not fwmark 1234 table 2468
sudo ip rule add table main suppress_prefixlength 0

trap - ERR INT TERM

exit 0
