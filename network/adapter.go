// Copyright 2024 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Helper methods for working with network adapters and binding outgoing traffic to a specific interface.

package network

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"
)

const DefaultServerIpAddress = "10.0.100.5"

// ServerIpAddress is the address driver stations attempt to contact. It defaults to the FRC standard, but is
// auto-updated to the selected adapter's IP if one is configured successfully.
var ServerIpAddress = DefaultServerIpAddress

type NetworkAdapter struct {
	Name         string
	IPv4Address  string
	AllIPv4Addrs []string
}

var (
	fieldAdapterName string
	fieldAdapterIp   net.IP
)

// ConfigureFieldNetworkAdapter sets the adapter name to bind to for field-facing traffic. If the adapter cannot be
// resolved or has no IPv4 address, Cheesy Arena will fall back to the system default routing.
func ConfigureFieldNetworkAdapter(adapterName string) {
	fieldAdapterName = adapterName
	ip, err := ResolveAdapterIPv4(adapterName)
	if err != nil && adapterName != "" {
		log.Printf(
			"Falling back to default routing for field traffic; adapter %q is unavailable: %v",
			adapterName,
			err,
		)
	}

	fieldAdapterIp = ip
	if fieldAdapterIp != nil {
		ServerIpAddress = fieldAdapterIp.String()
		log.Printf("Binding field traffic to adapter %q (%s).", fieldAdapterName, ServerIpAddress)
	} else {
		ServerIpAddress = DefaultServerIpAddress
	}
}

// ResolveAdapterIPv4 returns the preferred IPv4 address for the given adapter name.
func ResolveAdapterIPv4(adapterName string) (net.IP, error) {
	if adapterName == "" {
		return nil, nil
	}

	iface, err := net.InterfaceByName(adapterName)
	if err != nil {
		return nil, err
	}

	ip := selectPreferredIPv4(iface)
	if ip == nil {
		return nil, fmt.Errorf("adapter %q has no IPv4 address", adapterName)
	}

	return ip, nil
}

// ListNetworkAdapters returns a list of non-loopback, UP interfaces that have IPv4 addresses.
func ListNetworkAdapters() ([]NetworkAdapter, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	adapters := []NetworkAdapter{}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		var ipv4s []string
		for _, addr := range addrs {
			if ipNet, ok := addr.(*net.IPNet); ok {
				if ip := ipNet.IP.To4(); ip != nil {
					ipv4s = append(ipv4s, ip.String())
				}
			}
		}
		if len(ipv4s) == 0 {
			continue
		}

		adapters = append(
			adapters,
			NetworkAdapter{
				Name:         iface.Name,
				IPv4Address:  pickPrimaryIPv4(ipv4s),
				AllIPv4Addrs: ipv4s,
			},
		)
	}

	sort.Slice(
		adapters, func(i, j int) bool {
			return adapters[i].Name < adapters[j].Name
		},
	)
	return adapters, nil
}

// DialFieldNetwork returns a bound connection for the given network and address, using the selected adapter if set.
func DialFieldNetwork(network, address string) (net.Conn, error) {
	dialer := net.Dialer{LocalAddr: getLocalAddrForNetwork(network)}
	return dialer.Dial(network, address)
}

// DialFieldNetworkTimeout is the same as DialFieldNetwork but with a custom timeout.
func DialFieldNetworkTimeout(network, address string, timeout time.Duration) (net.Conn, error) {
	dialer := net.Dialer{LocalAddr: getLocalAddrForNetwork(network), Timeout: timeout}
	return dialer.Dial(network, address)
}

// FieldDialContext is a DialContext-compatible wrapper for HTTP transports.
func FieldDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	dialer := net.Dialer{LocalAddr: getLocalAddrForNetwork(network)}
	return dialer.DialContext(ctx, network, address)
}

// FieldHttpClient builds an HTTP client that reuses the field-bound dialer.
func FieldHttpClient(timeout time.Duration) *http.Client {
	transport := &http.Transport{DialContext: FieldDialContext}
	return &http.Client{Transport: transport, Timeout: timeout}
}

func selectPreferredIPv4(iface *net.Interface) net.IP {
	addrs, err := iface.Addrs()
	if err != nil {
		return nil
	}

	var selected net.IP
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok {
			if ip := ipNet.IP.To4(); ip != nil && !ip.IsLoopback() {
				if selected == nil || preferIPv4(ip, selected) {
					selected = ip
				}
			}
		}
	}
	return selected
}

func preferIPv4(candidate, current net.IP) bool {
	if strings.HasPrefix(candidate.String(), "10.") && !strings.HasPrefix(current.String(), "10.") {
		return true
	}
	if candidate.IsPrivate() && !current.IsPrivate() {
		return true
	}
	return false
}

func pickPrimaryIPv4(addresses []string) string {
	if len(addresses) == 0 {
		return ""
	}
	selected := net.ParseIP(addresses[0]).To4()
	for _, addr := range addresses[1:] {
		ip := net.ParseIP(addr).To4()
		if ip == nil {
			continue
		}
		if preferIPv4(ip, selected) {
			selected = ip
		}
	}
	return selected.String()
}

func getLocalAddrForNetwork(network string) net.Addr {
	if fieldAdapterIp == nil {
		return nil
	}

	if strings.HasPrefix(network, "udp") {
		return &net.UDPAddr{IP: fieldAdapterIp}
	}
	return &net.TCPAddr{IP: fieldAdapterIp}
}

// FieldAdapterIP returns a copy of the currently configured adapter IP, or nil if no adapter is locked.
func FieldAdapterIP() net.IP {
	if fieldAdapterIp == nil {
		return nil
	}
	return append(net.IP{}, fieldAdapterIp...)
}
