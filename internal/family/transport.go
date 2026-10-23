// How a start-up disk is made to find the ISO. Whatever the system, the ISO is offered in one of four ways, and a family picks the best one that it, and the server, can do.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package family

import "strconv"

// Transport is a way for the start-up disk to reach the ISO, from the best to the worst.
type Transport int

const (
	// NBD: the ISO is a network block disk that the extra archive attaches; the system reads it a piece at a time and needs no memory for it.
	NBD Transport = iota
	// ISCSI: the ISO is a network CD-ROM that the start-up disk logs in to itself (only dracut's has the client for it), also read a piece at a time.
	ISCSI
	// HTTP: only the system image (a few hundred MB) is downloaded, not the whole ISO.
	HTTP
	// Memory: the whole ISO is downloaded into the PC's memory.
	Memory
)

// has says whether the server can offer the ISO this way.
func (u URLs) has(t Transport) bool {
	switch t {
	case NBD:
		return u.NBD != "" && u.Extra != "" && u.NBDHost != ""
	case ISCSI:
		return u.ISCSIHost != ""
	}
	return true // HTTP and Memory need nothing but the web server
}

// pick is the first of the transports (in the order given) that the server can offer; the last one when it can offer none.
func (u URLs) pick(order ...Transport) Transport {
	for _, t := range order {
		if u.has(t) {
			return t
		}
	}
	return order[len(order)-1]
}

// nbdPort is the network disk server's port as a string, for the arguments that name it.
func (u URLs) nbdPort() string { return itoa(u.NBDPort) }

func itoa(n int) string { return strconv.Itoa(n) }
