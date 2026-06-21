# xcore-bridge

`xcore-bridge` runs `xray-core` as a Surge for Mac External Proxy helper. It turns VLESS share links into local SOCKS5 endpoints that Surge can start on demand.

The project is macOS-only because Surge for Mac is required. Release assets are built for:

- `darwin/amd64`
- `darwin/arm64`

## Install

Install the latest stable release, falling back to the newest prerelease when no stable release exists:

```sh
curl -fsSL https://raw.githubusercontent.com/orchiliao/xcore-bridge/main/scripts/install.sh | sh
```

Optional channel/version controls:

```sh
curl -fsSL https://raw.githubusercontent.com/orchiliao/xcore-bridge/main/scripts/install.sh | sh -s -- --beta
curl -fsSL https://raw.githubusercontent.com/orchiliao/xcore-bridge/main/scripts/install.sh | sh -s -- --stable
curl -fsSL https://raw.githubusercontent.com/orchiliao/xcore-bridge/main/scripts/install.sh | sh -s -- --version v0.1.0 --bindir "$HOME/.local/bin"
```

From a local checkout:

```sh
./scripts/install-macos.sh
go install ./cmd/xcore-bridge
```

## Surge Setup

Put one VLESS share link per line in `links.txt`. Blank lines and `#` comments are ignored.

```sh
xcore-bridge surge-install --links-file ./links.txt
```

When `--profile` is omitted, `surge-install` looks for Surge `.conf` profiles in iCloud Drive first, then in the local Surge profile directory. You can still choose a profile explicitly:

```sh
xcore-bridge surge-install \
  --profile "$HOME/Library/Application Support/Surge/Profiles/default.conf" \
  --links-file ./links.txt
```

On the first write to a profile, `surge-install` asks for confirmation and shows the exact path it will edit. Use `--yes` for non-interactive runs. Every write creates or replaces one backup beside the profile:

```text
profile.conf.bak
```

`surge-install` only edits the `[Proxy]` section. It replaces the block between these markers and leaves other proxy lines alone:

```ini
# xcore-bridge managed external proxies begin
# xcore-bridge managed external proxies end
```

Generated policies avoid existing policy names, profile-used `local-port` values, and currently occupied `127.0.0.1` TCP ports. Use `--dry-run` to print the updated profile without writing it.

## Commands

Generate one Surge `[Proxy]` line:

```sh
xcore-bridge surge-line --link 'vless://UUID@example.com:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=www.example.com&fp=chrome&pbk=PUBLIC_KEY&sid=0123abcd&type=tcp#Example'
```

Run one bridge directly:

```sh
xcore-bridge run --local-port 61080 --link 'vless://...'
```

Print the generated xray-core JSON:

```sh
xcore-bridge xray-config --local-port 61080 --link 'vless://...'
```

Check versions:

```sh
xcore-bridge version
xcore-bridge version --verbose
```

## Supported Links

The main supported target is VLESS + REALITY + Vision:

- `encryption=none`
- `flow=xtls-rprx-vision`
- `security=reality`
- `sni` / `serverName`
- `fp` / `fingerprint`
- `pbk` / `publicKey`
- `sid` / `shortId`
- `spx` / `spiderX`
- `type=tcp`

The generator also maps common stream fields for `tls`, `ws`, `grpc`, `httpupgrade`, and `splithttp`.

## Development

```sh
go test ./...
go build -trimpath -ldflags "-X main.version=dev" ./cmd/xcore-bridge
```

Build release-style macOS binaries:

```sh
for arch in amd64 arm64; do
  CGO_ENABLED=0 GOOS=darwin GOARCH="$arch" go build -trimpath -ldflags "-s -w -X main.version=dev" -o "dist/xcore-bridge-darwin-$arch" ./cmd/xcore-bridge
done
```
