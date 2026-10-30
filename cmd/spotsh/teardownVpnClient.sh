#!/bin/bash

sudo ip rule delete table main suppress_prefixlength 0 2>/dev/null || true
sudo ip rule delete not fwmark 1234 table 2468 2>/dev/null || true
sudo ip route flush table 2468 2>/dev/null || true
sudo ip link delete wg0 2>/dev/null || true
