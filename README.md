# Petri


## Development

### Local testnet
* Install
```sh
make install
```
* Setup network
```sh
furya testnet --chain-id=testing --output-dir=$(pwd)/testnet --v=2 --keyring-backend=test --commit-timeout=1500ms --minimum-gas-prices=""
```
* Start a validator node
```sh
furya start --home=./testnet/node0/furya
```

## License

Apache 2.0, see [LICENSE](./LICENSE) and [NOTICE](./NOTICE).
