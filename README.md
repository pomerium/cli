# Pomerium Command Line Client

The Pomerium Command Line Client (CLI) is a helper client-side application for [Pomerium](https://pomerium.com).

## Installation

Installation instructions are available [here](https://www.pomerium.com/docs/releases.html#pomerium-cli).

## Usage

The two CLI operations are:

1. Establishing [TCP tunnels](https://www.pomerium.com/docs/tcp/client.html) through Pomerium.
2. Generating `kubectl` credentials for [Kubernetes](https://www.pomerium.com/docs/k8s/).

```text
Usage:
  pomerium-cli [command]

Available Commands:
  completion  generate the autocompletion script for the specified shell
  help        Help about any command
  k8s         commands for the kubernetes credential plugin
  tcp         creates a TCP tunnel through Pomerium
  version     version

Flags:
  -h, --help      help for pomerium-cli
  -v, --version   version for pomerium-cli
```


