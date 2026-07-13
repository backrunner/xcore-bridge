package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/backrunner/xcore-bridge/internal/bridge"
	"github.com/backrunner/xcore-bridge/internal/vless"
	"github.com/xtls/xray-core/core"
)

const networkTestUUID = "00000000-0000-0000-0000-000000000001"

func TestNetworkHelper(t *testing.T) {
	switch os.Getenv("XCORE_BRIDGE_NETWORK_HELPER") {
	case "xray-server":
		runXrayServerHelper(t)
	case "managed-child":
		runManagedChildHelper(t)
	default:
		t.Skip("helper process only")
	}
}

func TestRepeatedManagedChildrenKeepSOCKSForwarding(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	targetAddress := startTCPEchoServer(t)
	udpTargetAddress := startUDPEchoServer(t)
	vlessPort := freeRunTCPPort(t)
	vlessServer := startNetworkHelper(t, map[string]string{
		"XCORE_BRIDGE_NETWORK_HELPER": "xray-server",
		"XCORE_BRIDGE_TEST_PORT":      strconv.Itoa(vlessPort),
	})
	waitForTCPListener(t, net.JoinHostPort("127.0.0.1", strconv.Itoa(vlessPort)), 3*time.Second)

	link := fmt.Sprintf("vless://%s@127.0.0.1:%d?encryption=none&security=none&type=tcp#Local", networkTestUUID, vlessPort)
	profile := "/tmp/xcore-bridge-network-test.conf"
	ports := []int{freeRunTCPPort(t), freeRunTCPPort(t), freeRunTCPPort(t)}
	children := make([]*networkHelperProcess, 0, len(ports))
	for _, port := range ports {
		child := startNetworkHelper(t, map[string]string{
			"XCORE_BRIDGE_NETWORK_HELPER": "managed-child",
			"XCORE_BRIDGE_TEST_LINK":      link,
			"XCORE_BRIDGE_TEST_PROFILE":   profile,
			"XCORE_BRIDGE_TEST_PORT":      strconv.Itoa(port),
		})
		children = append(children, child)
		if err := bridge.WaitForReady(context.Background(), "127.0.0.1", port, 3*time.Second); err != nil {
			t.Fatalf("child SOCKS5 listener %d did not become ready: %v\n%s", port, err, child.stderr.String())
		}
	}

	assertConcurrentSOCKSRoundTrips(t, ports, targetAddress)
	for _, port := range ports {
		assertSOCKSUDPRoundTrip(t, port, udpTargetAddress, []byte(fmt.Sprintf("udp-port-%d", port)))
	}

	standby := startNetworkHelper(t, map[string]string{
		"XCORE_BRIDGE_NETWORK_HELPER": "managed-child",
		"XCORE_BRIDGE_TEST_LINK":      link,
		"XCORE_BRIDGE_TEST_PROFILE":   profile,
		"XCORE_BRIDGE_TEST_PORT":      strconv.Itoa(ports[0]),
	})
	standby.AssertRunning(t, 300*time.Millisecond)
	assertSOCKSRoundTrip(t, ports[0], targetAddress, []byte("before-takeover"))

	children[0].Stop(t)
	eventuallyNetwork(t, 5*time.Second, func() bool {
		return socksRoundTrip(ports[0], targetAddress, []byte("after-takeover")) == nil
	})
	standby.AssertRunning(t, 100*time.Millisecond)
	assertSOCKSUDPRoundTrip(t, ports[0], udpTargetAddress, []byte("udp-after-takeover"))

	for i := 0; i < 20; i++ {
		payload := []byte(fmt.Sprintf("stable-forward-%02d", i))
		assertSOCKSRoundTrip(t, ports[0], targetAddress, payload)
	}

	vlessServer.AssertRunning(t, 100*time.Millisecond)
}

func runXrayServerHelper(t *testing.T) {
	port := requiredNetworkHelperPort(t)
	data, err := json.Marshal(map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []any{
			map[string]any{
				"listen":   "127.0.0.1",
				"port":     port,
				"protocol": "vless",
				"settings": map[string]any{
					"clients":    []any{map[string]any{"id": networkTestUUID}},
					"decryption": "none",
				},
				"streamSettings": map[string]any{
					"network":  "tcp",
					"security": "none",
				},
			},
		},
		"outbounds": []any{map[string]any{"protocol": "freedom", "tag": "direct"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := core.LoadConfig("json", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server, err := core.NewWithContext(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	<-ctx.Done()
}

func runManagedChildHelper(t *testing.T) {
	port := requiredNetworkHelperPort(t)
	node, err := vless.Parse(os.Getenv("XCORE_BRIDGE_TEST_LINK"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runManagedPolicy(ctx, node, os.Getenv("XCORE_BRIDGE_TEST_PROFILE"), "127.0.0.1", port, "warning", io.Discard); err != nil {
		t.Fatal(err)
	}
}

func requiredNetworkHelperPort(t *testing.T) int {
	t.Helper()
	port, err := strconv.Atoi(os.Getenv("XCORE_BRIDGE_TEST_PORT"))
	if err != nil || port <= 0 || port > 65535 {
		t.Fatalf("invalid helper port %q", os.Getenv("XCORE_BRIDGE_TEST_PORT"))
	}
	return port
}

type networkHelperProcess struct {
	cmd     *exec.Cmd
	done    chan error
	stderr  bytes.Buffer
	exited  bool
	exitErr error
}

func startNetworkHelper(t *testing.T, values map[string]string) *networkHelperProcess {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestNetworkHelper$")
	cmd.Env = os.Environ()
	for key, value := range values {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	process := &networkHelperProcess{cmd: cmd, done: make(chan error, 1)}
	cmd.Stdout = io.Discard
	cmd.Stderr = &process.stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		process.done <- cmd.Wait()
	}()
	t.Cleanup(func() {
		process.Stop(t)
	})
	return process
}

func (p *networkHelperProcess) AssertRunning(t *testing.T, duration time.Duration) {
	t.Helper()
	if p.exited {
		t.Fatalf("helper already exited: %v\n%s", p.exitErr, p.stderr.String())
	}
	select {
	case err := <-p.done:
		p.exited = true
		p.exitErr = err
		t.Fatalf("helper exited unexpectedly: %v\n%s", err, p.stderr.String())
	case <-time.After(duration):
	}
}

func (p *networkHelperProcess) Stop(t *testing.T) {
	t.Helper()
	if p == nil || p.exited {
		return
	}
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Errorf("stop helper: %v", err)
	}
	select {
	case err := <-p.done:
		p.exited = true
		p.exitErr = err
		if err != nil {
			t.Errorf("helper exit: %v\n%s", err, p.stderr.String())
		}
	case <-time.After(3 * time.Second):
		_ = p.cmd.Process.Kill()
		err := <-p.done
		p.exited = true
		p.exitErr = err
		t.Errorf("helper did not stop gracefully: %v\n%s", err, p.stderr.String())
	}
}

func startTCPEchoServer(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var connections sync.WaitGroup
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Add(1)
			go func() {
				defer connections.Done()
				defer conn.Close()
				buffer := make([]byte, 32*1024)
				for {
					n, err := conn.Read(buffer)
					if n > 0 {
						if _, writeErr := conn.Write(buffer[:n]); writeErr != nil {
							return
						}
					}
					if err != nil {
						return
					}
				}
			}()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-done
		connections.Wait()
	})
	return listener.Addr().String()
}

func startUDPEchoServer(t *testing.T) string {
	t.Helper()
	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 64*1024)
		for {
			n, address, err := listener.ReadFrom(buffer)
			if err != nil {
				return
			}
			if _, err := listener.WriteTo(buffer[:n], address); err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-done
	})
	return listener.LocalAddr().String()
}

func assertConcurrentSOCKSRoundTrips(t *testing.T, ports []int, targetAddress string) {
	t.Helper()
	errs := make(chan error, len(ports)*8)
	var wg sync.WaitGroup
	for _, port := range ports {
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func(port, attempt int) {
				defer wg.Done()
				payload := []byte(fmt.Sprintf("port-%d-attempt-%d", port, attempt))
				errs <- socksRoundTrip(port, targetAddress, payload)
			}(port, i)
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func assertSOCKSRoundTrip(t *testing.T, port int, targetAddress string, payload []byte) {
	t.Helper()
	if err := socksRoundTrip(port, targetAddress, payload); err != nil {
		t.Fatal(err)
	}
}

func assertSOCKSUDPRoundTrip(t *testing.T, port int, targetAddress string, payload []byte) {
	t.Helper()
	if err := socksUDPRoundTrip(port, targetAddress, payload); err != nil {
		t.Fatal(err)
	}
}

func socksRoundTrip(port int, targetAddress string, payload []byte) error {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if err != nil {
		return fmt.Errorf("dial SOCKS5 port %d: %w", port, err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return err
	}
	if err := negotiateSOCKS5(conn); err != nil {
		return err
	}
	host, rawPort, err := net.SplitHostPort(targetAddress)
	if err != nil {
		return err
	}
	targetIP := net.ParseIP(host).To4()
	if targetIP == nil {
		return fmt.Errorf("test target is not IPv4: %s", targetAddress)
	}
	targetPort, err := strconv.Atoi(rawPort)
	if err != nil {
		return err
	}
	request := []byte{0x05, 0x01, 0x00, 0x01}
	request = append(request, targetIP...)
	request = binary.BigEndian.AppendUint16(request, uint16(targetPort))
	if _, err := conn.Write(request); err != nil {
		return err
	}
	if _, _, err := readSOCKS5Reply(conn); err != nil {
		return err
	}
	if _, err := conn.Write(payload); err != nil {
		return err
	}
	reply := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, reply); err != nil {
		return err
	}
	if !bytes.Equal(reply, payload) {
		return fmt.Errorf("SOCKS5 payload mismatch: got %q want %q", reply, payload)
	}
	return nil
}

func socksUDPRoundTrip(port int, targetAddress string, payload []byte) error {
	control, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if err != nil {
		return fmt.Errorf("dial SOCKS5 UDP control port %d: %w", port, err)
	}
	defer control.Close()
	if err := control.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return err
	}
	if err := negotiateSOCKS5(control); err != nil {
		return err
	}
	if _, err := control.Write([]byte{0x05, 0x03, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}); err != nil {
		return err
	}
	relayHost, relayPort, err := readSOCKS5Reply(control)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(relayHost); ip != nil && ip.IsUnspecified() {
		relayHost = "127.0.0.1"
	}
	relayAddress, err := net.ResolveUDPAddr("udp", net.JoinHostPort(relayHost, strconv.Itoa(relayPort)))
	if err != nil {
		return err
	}
	udpConn, err := net.DialUDP("udp", nil, relayAddress)
	if err != nil {
		return err
	}
	defer udpConn.Close()
	if err := udpConn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return err
	}
	request, err := socks5UDPDatagram(targetAddress, payload)
	if err != nil {
		return err
	}
	if _, err := udpConn.Write(request); err != nil {
		return err
	}
	reply := make([]byte, 64*1024)
	n, err := udpConn.Read(reply)
	if err != nil {
		return err
	}
	replyPayload, err := socks5UDPPayload(reply[:n])
	if err != nil {
		return err
	}
	if !bytes.Equal(replyPayload, payload) {
		return fmt.Errorf("SOCKS5 UDP payload mismatch: got %q want %q", replyPayload, payload)
	}
	return nil
}

func negotiateSOCKS5(conn net.Conn) error {
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return err
	}
	greeting := make([]byte, 2)
	if _, err := io.ReadFull(conn, greeting); err != nil {
		return err
	}
	if greeting[0] != 0x05 || greeting[1] != 0x00 {
		return fmt.Errorf("unexpected SOCKS5 greeting response %x", greeting)
	}
	return nil
}

func readSOCKS5Reply(reader io.Reader) (string, int, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(reader, header); err != nil {
		return "", 0, err
	}
	if header[0] != 0x05 || header[1] != 0x00 {
		return "", 0, fmt.Errorf("SOCKS5 request failed with response %x", header)
	}
	var host string
	switch header[3] {
	case 0x01:
		address := make([]byte, net.IPv4len)
		if _, err := io.ReadFull(reader, address); err != nil {
			return "", 0, err
		}
		host = net.IP(address).String()
	case 0x04:
		address := make([]byte, net.IPv6len)
		if _, err := io.ReadFull(reader, address); err != nil {
			return "", 0, err
		}
		host = net.IP(address).String()
	case 0x03:
		length := []byte{0}
		if _, err := io.ReadFull(reader, length); err != nil {
			return "", 0, err
		}
		address := make([]byte, int(length[0]))
		if _, err := io.ReadFull(reader, address); err != nil {
			return "", 0, err
		}
		host = string(address)
	default:
		return "", 0, fmt.Errorf("unexpected SOCKS5 address type %d", header[3])
	}
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(reader, portBytes); err != nil {
		return "", 0, err
	}
	return host, int(binary.BigEndian.Uint16(portBytes)), nil
}

func socks5UDPDatagram(targetAddress string, payload []byte) ([]byte, error) {
	host, rawPort, err := net.SplitHostPort(targetAddress)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(host).To4()
	if ip == nil {
		return nil, fmt.Errorf("test UDP target is not IPv4: %s", targetAddress)
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil {
		return nil, err
	}
	datagram := []byte{0x00, 0x00, 0x00, 0x01}
	datagram = append(datagram, ip...)
	datagram = binary.BigEndian.AppendUint16(datagram, uint16(port))
	return append(datagram, payload...), nil
}

func socks5UDPPayload(datagram []byte) ([]byte, error) {
	if len(datagram) < 4 || datagram[0] != 0x00 || datagram[1] != 0x00 || datagram[2] != 0x00 {
		return nil, fmt.Errorf("invalid SOCKS5 UDP datagram %x", datagram)
	}
	offset := 4
	switch datagram[3] {
	case 0x01:
		offset += net.IPv4len
	case 0x04:
		offset += net.IPv6len
	case 0x03:
		if len(datagram) <= offset {
			return nil, fmt.Errorf("truncated SOCKS5 UDP domain")
		}
		offset += 1 + int(datagram[offset])
	default:
		return nil, fmt.Errorf("unexpected SOCKS5 UDP address type %d", datagram[3])
	}
	offset += 2
	if len(datagram) < offset {
		return nil, fmt.Errorf("truncated SOCKS5 UDP datagram")
	}
	return datagram[offset:], nil
}

func waitForTCPListener(t *testing.T, address string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("TCP listener %s did not become ready", address)
}

func eventuallyNetwork(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("network condition was not met before timeout")
}
