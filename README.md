# xcore-bridge

Add VLESS links to Surge for Mac as managed External Proxy policies.

`xcore-bridge` is macOS-only. It finds your Surge `.conf` profile, writes managed External Proxy policies, and lets Surge start one foreground `xcore-bridge run` process per active policy. That process embeds `xray-core`, hosts the local SOCKS5 inbound, and stays alive until Surge stops it.

## Quick Start

Install:

```sh
curl -fsSL https://raw.githubusercontent.com/backrunner/xcore-bridge/main/scripts/install.sh | sh
```

The installer shows each step inline. If `/usr/local/bin` needs administrator permission, it explains why before macOS asks for your password. Use `--bindir` to install somewhere else. Running the installer again upgrades an existing installation.

Or install the current beta with Homebrew:

```sh
brew tap backrunner/xcore-bridge https://github.com/backrunner/xcore-bridge
brew install backrunner/xcore-bridge/xcore-bridge-beta
```

This repository is the Homebrew tap, so the tap command includes the repository URL. After the first stable release, use `brew install backrunner/xcore-bridge/xcore-bridge` instead of the beta formula.

Upgrade:

```sh
xcore-bridge upgrade
```

For Homebrew installations, use Homebrew to upgrade:

```sh
brew upgrade backrunner/xcore-bridge/xcore-bridge-beta
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

By default, the managed policy name comes from the VLESS link name after `#`. Override it when needed:

```sh
xcore-bridge add --name 'My Node' 'vless://UUID@example.com:443?...#Example'
```

Add several links:

```sh
xcore-bridge add \
  'vless://UUID@example.com:443?...#Example 1' \
  'vless://UUID@example.org:443?...#Example 2'
```

When naming several links explicitly, repeat `--name` in the same order as the links.

Or put one link per line in `links.txt`:

```sh
xcore-bridge add --links-file ./links.txt
```

Remove a managed policy:

```sh
xcore-bridge remove 'Example'
xcore-bridge remove --name 'Example'
```

Rename a managed policy:

```sh
xcore-bridge rename 'Example' 'Example HK'
```

Replace a managed policy's VLESS link while keeping its Surge policy name and local port:

```sh
xcore-bridge replace 'Example' 'vless://UUID@example.com:443?...#Example'
```

Check and control the daemon:

```sh
xcore-bridge status
xcore-bridge daemon start
xcore-bridge daemon stop
xcore-bridge daemon restart
xcore-bridge daemon install
xcore-bridge daemon uninstall
```

Inspect logs when Surge or a managed policy fails to connect:

```sh
xcore-bridge log
xcore-bridge log --follow
xcore-bridge daemon log
xcore-bridge daemon log --follow
```

`xcore-bridge log` shows the foreground processes that Surge starts, including embedded xray-core access/error logs. `xcore-bridge daemon log` shows output from manual daemon commands and the manual daemon's embedded xray-core logs.

`daemon install` registers a user LaunchAgent for the manual daemon, starts it at login, and lets macOS restart it if it exits. This is separate from the default Surge External Proxy mode, where Surge owns each foreground policy process.

When the manual daemon is already running for the same profile, Surge-started `run` processes reuse that daemon's matching SOCKS5 listener instead of starting another embedded xray-core. Stop the daemon to return to the default foreground-per-policy mode.

Reload Surge after the profile is updated, then select the new policies in Surge.

## What It Does

- Finds Surge profiles in iCloud Drive first, then local Surge profiles.
- Asks for confirmation the first time it edits a profile.
- Creates one backup beside the profile: `profile.conf.bak`.
- Keeps generated policies inside its managed block in `[Proxy]`.
- Uses Surge External Proxy Program so Surge starts and stops a lightweight foreground bridge process.
- Runs xray-core inside that foreground process, exposes a local SOCKS5 inbound, and enables UDP relay.
- Routes each managed SOCKS5 inbound directly to its VLESS outbound. xray-core does not add domain/IP split-routing rules.
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

## Homebrew Tap

The root `Formula/` directory lets this repository act as a custom Homebrew tap:

```sh
brew tap backrunner/xcore-bridge https://github.com/backrunner/xcore-bridge
```

Release tags update the formula with `scripts/update-homebrew-formula.sh` after GitHub Release assets and `checksums.txt` are published. Stable tags update `Formula/xcore-bridge.rb`; prerelease tags update `Formula/xcore-bridge-beta.rb`.
