package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
)

// defaultHTTPAddress mirrors the HTTP_ADDRESS default in internal/config.
const defaultHTTPAddress = ":8080"

// healthcheck probes the local /healthz endpoint and returns a process exit
// code. It backs the container HEALTHCHECK, since the distroless runtime
// image has no shell or HTTP client.
func healthcheck(address string) int {
	url, err := healthURL(address)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthcheck: unexpected status", resp.Status)
		return 1
	}
	return 0
}

// healthURL maps the listen address to a loopback URL for /healthz.
func healthURL(address string) (string, error) {
	if address == "" {
		address = defaultHTTPAddress
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", fmt.Errorf("invalid HTTP_ADDRESS %q: %w", address, err)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/healthz", nil
}
