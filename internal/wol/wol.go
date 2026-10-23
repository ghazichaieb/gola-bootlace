// Package wol sends Wake-on-LAN "magic packets": six bytes of 0xFF and then the PC's MAC address sixteen times, in a UDP broadcast. A PC whose network card is set to wake on
// that packet starts up, and with a network boot first in its boot order it comes to Bootlace's menu.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package wol

import (
	"errors"
	"net"
)

// Packet is the magic packet for a MAC address.
func Packet(mac net.HardwareAddr) ([]byte, error) {
	if len(mac) != 6 {
		return nil, errors.New("a MAC address has six bytes")
	}
	p := make([]byte, 0, 102)
	for i := 0; i < 6; i++ {
		p = append(p, 0xFF)
	}
	for i := 0; i < 16; i++ {
		p = append(p, mac...)
	}
	return p, nil
}

// Broadcast is the broadcast address of the network that has this address and mask.
func Broadcast(ip net.IP, mask net.IPMask) net.IP {
	ip4 := ip.To4()
	if ip4 == nil || len(mask) != 4 {
		return net.IPv4bcast
	}
	b := make(net.IP, 4)
	for i := range b {
		b[i] = ip4[i] | ^mask[i]
	}
	return b
}

// Send broadcasts the magic packet for a MAC address from the local address to each destination ("host:port"; port 9 is the usual one). It succeeds when at least one
// destination took it.
func Send(mac net.HardwareAddr, local net.IP, destinations ...string) error {
	p, err := Packet(mac)
	if err != nil {
		return err
	}
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: local})
	if err != nil {
		return err
	}
	defer c.Close()
	var last error
	sent := 0
	for _, d := range destinations {
		to, err := net.ResolveUDPAddr("udp4", d)
		if err != nil {
			last = err
			continue
		}
		if _, err := c.WriteToUDP(p, to); err != nil {
			last = err
			continue
		}
		sent++
	}
	if sent == 0 {
		if last == nil {
			last = errors.New("no destination")
		}
		return last
	}
	return nil
}
