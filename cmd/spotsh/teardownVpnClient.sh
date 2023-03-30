#!/bin/bash

sudo ip rule delete table main suppress_prefixlength 0
sudo ip rule delete not fwmark 1234 table 2468
sudo ip route flush table 2468
sudo ip link set wg0 down
