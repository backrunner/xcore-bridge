# xcore-bridge

Add VLESS links to Surge for Mac as External Proxy policies.

`xcore-bridge` is macOS-only. It automatically finds your Surge `.conf` profile, updates the `[Proxy]` section, and lets Surge start an embedded `xray-core` bridge on demand.

## Quick Start

Install:

```sh
curl -fsSL https://raw.githubusercontent.com/backrunner/xcore-bridge/main/scripts/install.sh | sh
```

Add one VLESS link to Surge:

```sh
xcore-bridge surge-install 'vless://UUID@example.com:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=www.example.com&fp=chrome&pbk=PUBLIC_KEY&sid=0123abcd&type=tcp#Example'
```

Add several links:

```sh
xcore-bridge surge-install \
  'vless://UUID@example.com:443?...#Example 1' \
  'vless://UUID@example.org:443?...#Example 2'
```

Or put one link per line in `links.txt`:

```sh
xcore-bridge surge-install --links-file ./links.txt
```

Restart or reload Surge after the profile is updated, then select the new policies in Surge.

## What It Does

- Finds Surge profiles in iCloud Drive first, then local Surge profiles.
- Asks for confirmation the first time it edits a profile.
- Creates one backup beside the profile: `profile.conf.bak`.
- Replaces only its managed block inside `[Proxy]`.
- Chooses local ports automatically and avoids conflicts.

If multiple profiles exist, `xcore-bridge` uses the first discovered profile and prints the exact path before editing. To choose manually:

```sh
xcore-bridge surge-install \
  --profile "$HOME/Library/Application Support/Surge/Profiles/default.conf" \
  'vless://UUID@example.com:443?...#Example'
```

## Notes

Generated Surge proxy lines contain the full VLESS share link. Treat your Surge profile as sensitive.

For non-interactive setup:

```sh
xcore-bridge surge-install --yes 'vless://UUID@example.com:443?...#Example'
```

Preview without writing:

```sh
xcore-bridge surge-install --dry-run 'vless://UUID@example.com:443?...#Example'
```

## Development

```sh
go test ./...
go build -trimpath -ldflags "-X main.version=dev" ./cmd/xcore-bridge
```
