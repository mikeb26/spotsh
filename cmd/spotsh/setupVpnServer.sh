#!/bin/bash

CLIENT_PUB_KEY=$1
SERVER_PUB_KEY_FILE=$2

if [ "$CLIENT_PUB_KEY" == "" ] || [ "$SERVER_PUB_KEY_FILE" == "" ] || [ "$3" != "" ]
then
    printf "Usage:\n\tsetupVpnServer.sh <clientPubKey> <serverPubKeyFile>\n" 1>&2
    exit 1
fi

CLIENT_PUB_IP=$(echo $SSH_CLIENT | cut -f1 -d' ')
if [ "$CLIENT_PUB_IP" == "" ]
then
    printf "Cannot find client public ip" 1>&2
    exit 1
fi

SERVER_VPN_IP=10.226.0.1
CLIENT_VPN_IP=10.226.0.2
VPN_PORT=26026
ETH0="ens5"
WG0="wg0"
SERVER_PRIV_KEY_FILE="vpn.server.key.private"


sudo dnf -y install gcc git make iptables
git clone https://git.zx2c4.com/wireguard-tools
make -C wireguard-tools/src -j$(nproc)
sudo make -C wireguard-tools/src install
if [ ! -e "$SERVER_PRIV_KEY_FILE" ]
then
    wg genkey > $SERVER_PRIV_KEY_FILE
    chmod 400 $SERVER_PRIV_KEY_FILE
fi
if [ ! -e "$SERVER_PUB_KEY_FILE" ]
then
    wg pubkey < $SERVER_PRIV_KEY_FILE > $SERVER_PUB_KEY_FILE
fi

sudo ip link add dev $WG0 type wireguard
sudo ip addr add $SERVER_VPN_IP/24 dev $WG0
sudo wg set $WG0 private-key ./$SERVER_PRIV_KEY_FILE
sudo wg set $WG0 listen-port $VPN_PORT
sudo wg set $WG0 peer $CLIENT_PUB_KEY allowed-ips $CLIENT_VPN_IP/32 endpoint $CLIENT_PUB_IP:$VPN_PORT persistent-keepalive 25
sudo ip link set $WG0 up
sudo sh -c "echo '1' > /proc/sys/net/ipv4/ip_forward"
sudo iptables -A FORWARD -i $WG0 -j ACCEPT
sudo iptables -t nat -A POSTROUTING -o $ETH0 -j MASQUERADE

exit 0
