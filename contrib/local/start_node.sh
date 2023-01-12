#!/bin/bash
set -eu

furya start --rpc.laddr tcp://0.0.0.0:26657 --log_level=info --trace
