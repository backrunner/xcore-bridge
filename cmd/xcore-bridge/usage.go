package main

import (
	"fmt"
	"io"
)

func printUsage(w io.Writer) {
	fmt.Fprint(w, `xcore-bridge wraps xray-core as a Surge External Proxy program.

Usage:
  xcore-bridge run --local-port 61080 --link 'vless://...'
  xcore-bridge add 'vless://...'
  xcore-bridge remove 'Policy Name'
  xcore-bridge upgrade --channel stable
  xcore-bridge xray-config --local-port 61080 --link 'vless://...'

Commands:
  run            start one local SOCKS5 inbound and forward everything to the VLESS node
  add            add VLESS links as managed Surge External Proxy policies
  remove         remove managed Surge External Proxy policies by name
  upgrade        upgrade xcore-bridge from GitHub Releases; channel: auto, stable, or beta
  xray-config    print the generated xray-core JSON config
  version        print version
`)
}
