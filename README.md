# xcore-bridge

Add VLESS links to Surge for Mac as managed External Proxy policies.

`xcore-bridge` is macOS-only. It finds your Surge `.conf` profile, writes managed External Proxy policies, and lets Surge own the proxy process lifecycle. Each active managed policy runs one foreground `xcore-bridge` process with an embedded `xray-core` SOCKS5 inbound.

## Quick Start

Install:

```sh
curl -fsSL https://raw.githubusercontent.com/backrunner/xcore-bridge/main/scripts/install.sh | sh
```

The installer shows each step inline. If `/usr/local/bin` needs administrator permission, it explains why before macOS asks for your password. Use `--bindir` to install somewhere else. Running the installer again upgrades an existing installation.

Upgrade:

```sh
xcore-bridge upgrade
```

By default, `upgrade` uses the `auto` channel: latest stable first, then the newest prerelease only when no stable release exists. Choose a channel explicitly when needed:

```sh
xcore-bridge upgrade --channel stable
xcore-bridge upgrade --channel beta
```

To install a specific release tag:

```sh
xcore-bridge upgrade --version v1.2.3
```

Add one VLESS link to Surge:

```sh
xcore-bridge add 'vless://UUID@example.com:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=www.example.com&fp=chrome&pbk=PUBLIC_KEY&sid=0123abcd&type=tcp#Example'
```

Add several links:

```sh
xcore-bridge add \
  'vless://UUID@example.com:443?...#Example 1' \
  'vless://UUID@example.org:443?...#Example 2'
```

Or put one link per line in `links.txt`:

```sh
xcore-bridge add --links-file ./links.txt
```

Remove a managed policy:

```sh
xcore-bridge remove 'Example'
```

Reload Surge after the profile is updated, then select the new policies in Surge.

## What It Does

- Finds Surge profiles in iCloud Drive first, then local Surge profiles.
- Asks for confirmation the first time it edits a profile.
- Creates one backup beside the profile: `profile.conf.bak`.
- Keeps generated policies inside its managed block in `[Proxy]`.
- Uses Surge External Proxy Program so Surge starts and stops the bridge process.
- Runs xray-core inside that process, exposes a local SOCKS5 inbound, and enables UDP relay.
- Chooses local ports automatically and avoids TCP/UDP conflicts.

If multiple profiles exist, `xcore-bridge` uses the first discovered profile and prints the exact path before editing. To choose manually:

```sh
xcore-bridge add \
  --profile "$HOME/Library/Application Support/Surge/Profiles/default.conf" \
  'vless://UUID@example.com:443?...#Example'
```

## Notes

Generated Surge proxy lines contain the full VLESS share link. Treat your Surge profile as sensitive.

For non-interactive setup:

```sh
xcore-bridge add --yes 'vless://UUID@example.com:443?...#Example'
```

Preview without writing:

```sh
xcore-bridge add --dry-run 'vless://UUID@example.com:443?...#Example'
```

## Development

```sh
go test ./...
go build -trimpath -ldflags "-X main.version=dev" ./cmd/xcore-bridge
```
