package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	appconfig "heph4estus/internal/config"
	"heph4estus/internal/fleet"
	"heph4estus/internal/logger"
	"heph4estus/internal/tlsutil"

	"github.com/nats-io/nats.go"
)

// startHeartbeat launches a background goroutine that publishes fleet
// heartbeat messages over NATS. It returns a cancel function to stop
// the heartbeat.
func startHeartbeat(ctx context.Context, cfg *appconfig.WorkerConfig, log logger.Logger) (cancel func()) {
	if !cfg.FleetHeartbeat || cfg.NATSURL == "" {
		return func() {}
	}

	opts, err := heartbeatNATSOptions(cfg)
	if err != nil {
		log.Error("Fleet heartbeat: invalid NATS TLS trust: %v", err)
		return func() {}
	}
	opts = append(opts,
		nats.Name("heph-worker-heartbeat-"+cfg.WorkerID),
		nats.MaxReconnects(-1),
	)
	conn, err := nats.Connect(cfg.NATSURL, opts...)
	if err != nil {
		log.Error("Fleet heartbeat: failed to connect to NATS: %v", err)
		return func() {}
	}

	// Probe IPv6 connectivity once at startup.
	ipv6Ready := probeIPv6()
	publicIPv4, publicIPv6 := detectPublicIPs()

	hbCtx, hbCancel := context.WithCancel(ctx)

	go func() {
		ticker := time.NewTicker(fleet.DefaultHeartbeatInterval)
		defer ticker.Stop()
		defer conn.Close()

		// Publish initial heartbeat immediately.
		publishHeartbeat(conn, cfg, publicIPv4, publicIPv6, ipv6Ready, log)

		for {
			select {
			case <-hbCtx.Done():
				return
			case <-ticker.C:
				publishHeartbeat(conn, cfg, publicIPv4, publicIPv6, ipv6Ready, log)
			}
		}
	}()

	return hbCancel
}

func heartbeatNATSOptions(cfg *appconfig.WorkerConfig) ([]nats.Option, error) {
	tlsConfig, err := tlsutil.ClientConfigWithServerName(cfg.ControllerCAPEM, cfg.ControllerCAFile, cfg.ControllerServerName)
	if err != nil {
		return nil, fmt.Errorf("controller CA: %w", err)
	}
	if tlsConfig == nil {
		return nil, nil
	}
	return []nats.Option{nats.Secure(tlsConfig)}, nil
}

func publishHeartbeat(conn *nats.Conn, cfg *appconfig.WorkerConfig, ipv4, ipv6 string, ipv6Ready bool, log logger.Logger) {
	msg := fleet.HeartbeatMessage{
		WorkerID:     cfg.WorkerID,
		Host:         cfg.WorkerHost,
		PublicIPv4:   ipv4,
		PublicIPv6:   ipv6,
		IPv6Ready:    ipv6Ready,
		Version:      cfg.WorkerVersion,
		Ready:        true,
		Cloud:        cfg.Cloud,
		GenerationID: cfg.GenerationID,
		Timestamp:    time.Now().Unix(),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Error("Fleet heartbeat: marshal error: %v", err)
		return
	}

	if err := conn.Publish(fleet.HeartbeatSubject, data); err != nil {
		log.Error("Fleet heartbeat: publish error: %v", err)
	}
}

// probeIPv6 tests IPv6 connectivity by dialing a well-known IPv6 address.
// Returns true if the connection succeeds from inside the container.
func probeIPv6() bool {
	// Try to establish a UDP "connection" to Google's public DNS over IPv6.
	// This doesn't send any data — it just checks if the OS can route IPv6.
	conn, err := net.DialTimeout("udp6", "[2001:4860:4860::8888]:53", 5*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// detectPublicIPs attempts to determine the host's public IPv4 and IPv6
// addresses by examining network interfaces.
func detectPublicIPs() (ipv4, ipv6 string) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", ""
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() || ipNet.IP.IsPrivate() || ipNet.IP.IsLinkLocalUnicast() {
			continue
		}
		if ipNet.IP.To4() != nil && ipv4 == "" {
			ipv4 = ipNet.IP.String()
		} else if ipNet.IP.To4() == nil && ipv6 == "" {
			ipv6 = ipNet.IP.String()
		}
	}
	return ipv4, ipv6
}
