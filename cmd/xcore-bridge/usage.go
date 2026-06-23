package main

import (
	"fmt"
	"io"
)

func printUsage(w io.Writer) {
	fmt.Fprint(w, `xcore-bridge wraps xray-core as a Surge External Proxy program.

Usage:
  xcore-bridge run --local-port 61080 --link 'vless://...'
  xcore-bridge add [--name 'Policy Name'] 'vless://...'
  xcore-bridge remove [--name 'Policy Name'] ['Policy Name']
  xcore-bridge rename 'Old Name' 'New Name'
  xcore-bridge status
  xcore-bridge log
  xcore-bridge daemon start|stop|restart
  xcore-bridge daemon log
  xcore-bridge upgrade --channel stable
  xcore-bridge xray-config --local-port 61080 --link 'vless://...'

Commands:
  run            keep a Surge External Proxy process alive and ensure the daemon is ready
  add            add VLESS links as managed Surge External Proxy policies
  remove         remove managed Surge External Proxy policies by name
  rename         rename one managed Surge External Proxy policy
  status         show xcore-bridge daemon status
  log            show xcore-bridge bridge/supervisor logs
  daemon         manually start, stop, restart, or inspect the xcore-bridge daemon
  upgrade        upgrade xcore-bridge from GitHub Releases; channel: auto, stable, or beta
  xray-config    print the generated xray-core JSON config
  version        print version
`)
}
