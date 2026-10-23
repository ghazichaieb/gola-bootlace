// Package selftest checks the server from its own address the way a PC uses it, one step at a time, so that a service that is not listening, a port that something
// else holds, or the local-network rule that turns the server's own address away shows up without starting a PC. A firewall in front of the server's computer cannot be
// seen this way: the connections are made from the computer itself.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package selftest

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Target says where the services are. A port of 0 means the service is switched off and its step is skipped.
type Target struct {
	IP      string
	HTTP    int
	SMB     int
	NBD     int
	ISCSI   int
	TFTP    int    // (69 when 0)
	ISOPath string // the web path of one ISO (/iso/name.iso), "" when the library has none
}

// Result is the outcome of one step.
type Result struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`         // "ok", "fail" or "skipped"
	Detail string `json:"detail"`         // what was seen
	Hint   string `json:"hint,omitempty"` // what to look at when it failed
}

const wait = 4 * time.Second

// Run does every step at the same time and returns them in a fixed order.
func Run(ctx context.Context, t Target) []Result {
	steps := []func(context.Context, Target) Result{menu, iso, tftp, smb, nbd, iscsi}
	out := make([]Result, len(steps))
	var wg sync.WaitGroup
	for i, f := range steps {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = f(ctx, t)
		}()
	}
	wg.Wait()
	return out
}

func ok(id, title, detail string) Result {
	return Result{ID: id, Title: title, Status: "ok", Detail: detail}
}
func skipped(id, title, why string) Result {
	return Result{ID: id, Title: title, Status: "skipped", Detail: why}
}
func fail(id, title, detail, hint string) Result {
	return Result{ID: id, Title: title, Status: "fail", Detail: detail, Hint: hint}
}

func addr(ip string, port int) string { return net.JoinHostPort(ip, strconv.Itoa(port)) }

func get(ctx context.Context, u string, header map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	return (&http.Client{Timeout: wait}).Do(req)
}

func menu(ctx context.Context, t Target) Result {
	const id, title = "menu", "Boot menu (HTTP)"
	base := "http://" + addr(t.IP, t.HTTP)
	q := url.Values{"mac": {"00:00:00:00:00:01"}, "ip": {t.IP}, "plat": {"efi"}, "page": {"1"}} // (page 1: not noted as a PC that started)
	for _, path := range []string{"/boot.ipxe", "/menu?" + q.Encode()} {
		res, err := get(ctx, base+path, nil)
		if err != nil {
			return fail(id, title, err.Error(), "The web port does not answer from the server's own address. Is the program running with the address you expect (the Server address on the Dashboard)?")
		}
		body, _ := io.ReadAll(io.LimitReader(res.Body, 64))
		res.Body.Close()
		if res.StatusCode == http.StatusForbidden {
			return fail(id, title, "the server turned its own address away (403)", "Allow the network of the server under Server > Who may use the server: a PC on a network that is not allowed gets the same answer.")
		}
		if res.StatusCode != 200 || !bytes.HasPrefix(body, []byte("#!ipxe")) {
			return fail(id, title, fmt.Sprintf("%s answered %d", path, res.StatusCode), "The menu should start with #!ipxe. See the log.")
		}
	}
	return ok(id, title, "the boot script and the menu are served")
}

func iso(ctx context.Context, t Target) Result {
	const id, title = "iso", "ISO download (first bytes)"
	if t.ISOPath == "" {
		return skipped(id, title, "no ISO in the library to try")
	}
	res, err := get(ctx, "http://"+addr(t.IP, t.HTTP)+t.ISOPath, map[string]string{"Range": "bytes=0-15"})
	if err != nil {
		return fail(id, title, err.Error(), "The web port does not answer.")
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 64))
	if res.StatusCode != http.StatusPartialContent && res.StatusCode != http.StatusOK {
		return fail(id, title, fmt.Sprintf("answered %d", res.StatusCode), "Is the library available (Server page)? A library on a network folder that went away gives this.")
	}
	if len(body) < 16 {
		return fail(id, title, fmt.Sprintf("got %d byte(s) of 16", len(body)), "The file may be empty or still being copied.")
	}
	return ok(id, title, "the first 16 bytes of "+t.ISOPath+" were read")
}

func dial(ctx context.Context, ip string, port int) (net.Conn, error) {
	d := net.Dialer{Timeout: wait}
	c, err := d.DialContext(ctx, "tcp", addr(ip, port))
	if err == nil {
		_ = c.SetDeadline(time.Now().Add(wait))
	}
	return c, err
}

const notListening = "Nothing answers on this port. Is the service started (Server page)? Is another program using the port?"

func nbd(ctx context.Context, t Target) Result {
	const id, title = "nbd", "Network disk (handshake)"
	if t.NBD == 0 {
		return skipped(id, title, "switched off")
	}
	c, err := dial(ctx, t.IP, t.NBD)
	if err != nil {
		return fail(id, title, err.Error(), notListening)
	}
	defer c.Close()
	greeting := make([]byte, 16)
	if _, err := io.ReadFull(c, greeting); err != nil {
		return fail(id, title, "no greeting: "+err.Error(), "The server accepted the connection but did not speak the network disk protocol.")
	}
	if string(greeting[:8]) != "NBDMAGIC" || string(greeting[8:16]) != "IHAVEOPT" {
		return fail(id, title, fmt.Sprintf("an unexpected greeting %q", greeting), "Something else may be listening on this port.")
	}
	return ok(id, title, "the server greeted with NBDMAGIC")
}

func iscsi(ctx context.Context, t Target) Result {
	const id, title = "iscsi", "iSCSI target (port open)"
	if t.ISCSI == 0 {
		return skipped(id, title, "switched off")
	}
	c, err := dial(ctx, t.IP, t.ISCSI)
	if err != nil {
		return fail(id, title, err.Error(), notListening)
	}
	c.Close()
	return ok(id, title, "the port accepts connections")
}

// smb sends what Windows' set-up sends first, an SMB 1 negotiate that offers SMB 2, and wants an SMB 2 message back.
func smb(ctx context.Context, t Target) Result {
	const id, title = "smb", "Windows file share (first message)"
	if t.SMB == 0 {
		return skipped(id, title, "switched off")
	}
	c, err := dial(ctx, t.IP, t.SMB)
	if err != nil {
		return fail(id, title, err.Error(), notListening+" On Windows, port 445 is held by the system's own file sharing unless that is turned off.")
	}
	defer c.Close()
	if _, err := c.Write(smb1Negotiate()); err != nil {
		return fail(id, title, err.Error(), "")
	}
	head := make([]byte, 8)
	if _, err := io.ReadFull(c, head); err != nil {
		return fail(id, title, "no answer: "+err.Error(), "The connection was accepted but nothing was said back.")
	}
	if !bytes.Equal(head[4:8], []byte{0xFE, 'S', 'M', 'B'}) {
		return fail(id, title, fmt.Sprintf("an unexpected answer % x", head), "Something else may be listening on this port.")
	}
	return ok(id, title, "the share answered with an SMB 2 message")
}

func smb1Negotiate() []byte {
	dialects := []byte("\x02NT LM 0.12\x00\x02SMB 2.002\x00\x02SMB 2.???\x00")
	hdr := make([]byte, 32)
	copy(hdr, "\xFFSMB")
	hdr[4] = 0x72 // negotiate
	hdr[9] = 0x18 // flags
	binary.LittleEndian.PutUint16(hdr[10:], 0xC853)
	body := []byte{0, 0, 0} // word count 0, then the byte count
	binary.LittleEndian.PutUint16(body[1:], uint16(len(dialects)))
	msg := append(append(hdr, body...), dialects...)
	pkt := make([]byte, 4, 4+len(msg))
	binary.BigEndian.PutUint32(pkt, uint32(len(msg)))
	return append(pkt, msg...)
}

// tftp asks for a small file of the program and stops after the first answer. The answer comes from a new port of the server, so the socket is not connected.
func tftp(_ context.Context, t Target) Result {
	const id, title = "tftp", "TFTP (iPXE file)"
	port := t.TFTP
	if port == 0 {
		port = 69
	}
	server, err := net.ResolveUDPAddr("udp", addr(t.IP, port))
	if err != nil {
		return fail(id, title, err.Error(), "")
	}
	conn, err := net.ListenPacket("udp", ":0")
	if err != nil {
		return fail(id, title, err.Error(), "")
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(wait))
	if _, err := conn.WriteTo([]byte("\x00\x01snponly.efi\x00octet\x00tsize\x000\x00"), server); err != nil {
		return fail(id, title, err.Error(), "")
	}
	buf := make([]byte, 1500)
	n, from, err := conn.ReadFrom(buf)
	if err != nil {
		return fail(id, title, "no answer within "+wait.String(), "Is the TFTP service started (Server page)? Something else may hold UDP port 69.")
	}
	_, _ = conn.WriteTo([]byte("\x00\x05\x00\x00test over\x00"), from) // (an error packet ends the transfer on the server's side)
	return decodeTFTP(id, title, buf[:n])
}

func decodeTFTP(id, title string, pkt []byte) Result {
	if len(pkt) < 2 {
		return fail(id, title, "a short answer", "")
	}
	switch op := binary.BigEndian.Uint16(pkt); op {
	case 6:
		return ok(id, title, "snponly.efi was offered ("+strings.ReplaceAll(strings.TrimRight(string(pkt[2:]), "\x00"), "\x00", " ")+")")
	case 3:
		return ok(id, title, "snponly.efi was offered (first block received)")
	case 5:
		return fail(id, title, "the server refused: "+strings.TrimRight(string(pkt[4:]), "\x00"), "Is the server's own address among the allowed networks?")
	default:
		return fail(id, title, fmt.Sprintf("an unexpected TFTP packet (opcode %d)", op), "")
	}
}
