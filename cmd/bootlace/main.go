// Gola Bootlace: a network boot server for ISO files.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/access"
	"github.com/ghazichaieb/gola-bootlace/internal/admin"
	"github.com/ghazichaieb/gola-bootlace/internal/backup"
	"github.com/ghazichaieb/gola-bootlace/internal/config"
	"github.com/ghazichaieb/gola-bootlace/internal/dhcp"
	"github.com/ghazichaieb/gola-bootlace/internal/handoff"
	"github.com/ghazichaieb/gola-bootlace/internal/httpd"
	"github.com/ghazichaieb/gola-bootlace/internal/importer"
	"github.com/ghazichaieb/gola-bootlace/internal/isos"
	"github.com/ghazichaieb/gola-bootlace/internal/library"
	"github.com/ghazichaieb/gola-bootlace/internal/logbuf"
	"github.com/ghazichaieb/gola-bootlace/internal/pxecheck"
	"github.com/ghazichaieb/gola-bootlace/internal/selftest"
	"github.com/ghazichaieb/gola-bootlace/internal/service"
	"github.com/ghazichaieb/gola-bootlace/internal/smbshare"
	"github.com/ghazichaieb/gola-bootlace/internal/tidy"
	"github.com/ghazichaieb/gola-bootlace/internal/version"
	"github.com/ghazichaieb/gola-bootlace/internal/winpe"
	"github.com/ghazichaieb/gola-bootlace/internal/wol"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

const usage = `Gola Bootlace: boots PCs over the network from the ISO files in a folder.

  bootlace serve [options]   start the server (answers PXE PCs, serves the boot menu and the ISO files)
  bootlace inspect <iso>...  say what is inside ISO files and how the server would start them
  bootlace -version          print the version

  bootlace health            ask a running server whether it is well (exit status 0 when it is); for a container's health check

Every option of "bootlace serve" can also be set in the environment as BOOTLACE_<NAME> (BOOTLACE_HTTP_PORT, BOOTLACE_DHCP, BOOTLACE_ADMIN_PASSWORD ...);
the command line wins.

Run "bootlace serve -h" for the options.
`

// run is main without the process exit, so tests can call it.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "serve" {
		return serve(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "health" {
		return health(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "inspect" {
		return inspect(args[1:], stdout, stderr)
	}
	fs := flag.NewFlagSet("bootlace", flag.ContinueOnError)
	fs.SetOutput(stderr)
	showVersion := fs.Bool("version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Fprintln(stdout, version.String())
		return 0
	}
	fmt.Fprint(stderr, usage)
	return 1
}

func serve(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("bootlace serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	isoDir := fs.String("iso-dir", "", "the library, the folder with the ISO files, which must be readable and writable: only used the first time, to add it (after that the library is the one chosen in the web interface)")
	ipFlag := fs.String("ip", "", "this server's address as the PCs reach it (default: the first private address of this computer)")
	httpPort := fs.Int("http-port", 8080, "port of the web server")
	mode := fs.String("dhcp", "proxy", `"proxy": answer PXE PCs only, next to the network's own DHCP server; "full": also hand out addresses (only on a network without a DHCP server); "off": no DHCP at all`)
	first := fs.String("range-start", "", "full mode: first address to hand out (default: .100 of this network)")
	last := fs.String("range-end", "", "full mode: last address to hand out (default: .200 of this network)")
	mask := fs.String("mask", "255.255.255.0", "full mode: the network mask")
	router := fs.String("router", "", "full mode: the router given to PCs (default: none)")
	dnsIP := fs.String("dns", "", "full mode: the DNS server given to PCs (default: none)")
	pxeOnly := fs.Bool("pxe-only", false, "full mode: hand out addresses to PCs that start a network boot only (a lab where this computer is also on the network)")
	menuDefault := fs.Int("menu-default", 0, "the menu entry (counting from 1) that starts by itself after -menu-timeout seconds (0: wait for a choice)")
	smbPasswordFlag := fs.String("smb-password", "", "the password Windows set-up logs in to the share with (default: made up once and kept in the settings file)")
	nbdPort := fs.Int("nbd-port", 10809, "the port of the network disk server that a live Linux (Ubuntu) reads its ISO from instead of copying it into memory (0: off)")
	iscsiPort := fs.Int("iscsi-port", 3260, "the port of the iSCSI target that shows an ISO Bootlace cannot start piece by piece as a CD-ROM (0: off, the ISO is then downloaded whole)")
	wimlibFlag := fs.String("wimlib", "", "wimlib-imagex, which starts Windows with UEFI Secure Boot on (default: the one on the PATH)")
	smbForward := fs.String("smb-forward", "", "for finding faults: pass the share connections on to this SMB server (host:port) and log them")
	smbPort := fs.Int("smb-port", 445, "the port of the share that Windows set-up reads its files from")
	tftpAhead := fs.Int("tftp-window", 4, "how many TFTP blocks to send ahead of the acknowledgements to FreeBSD's loader (pfSense, OPNsense), which reads the whole ISO by TFTP, one block at a time otherwise; 1 turns it off")
	menuLocal := fs.Bool("menu-local", false, "after -menu-timeout seconds the PC goes on to its own disk instead of waiting (useful after an installation has restarted it)")
	menuPick := fs.String("menu-pick", "", "the entry whose name contains this text starts by itself after -menu-timeout seconds (instead of -menu-default)")
	menuTimeout := fs.Int("menu-timeout", 20, "seconds before the default menu entry starts (0: wait for ever); the default entry is the PC's own disk unless another is chosen")
	configPath := fs.String("config", "bootlace.json", "the file where the settings made in the web interface are kept (the menu options above only give its first contents)")
	adminPassword := fs.String("admin-password", "", "the password of the web interface (default: made up the first time and written in the log)")
	logFile := fs.String("log-file", "", "also write the log to this file (kept at the size of -log-size, with one older file next to it); the generated password is never written to it")
	logSize := fs.Int("log-size", 5, "the size in MB at which the log file is replaced by a new one")
	noAdmin := fs.Bool("no-admin", false, "do not offer the web interface")
	sbDir := fs.String("secure-boot-dir", "", "PROTOTYPE, off unless given: a folder with shimx64.efi (a distribution's Microsoft-signed shim), grubx64.efi (iPXE signed with your own key, tools/ipxe/build-sb.sh) and mmx64.efi (MokManager), for PCs with UEFI Secure Boot on (see docs/secure-boot.md); a PC is sent through the shim when it is marked so in the web interface or with -secure-boot-all")
	sbShim := fs.String("secure-boot-shim", httpd.DefaultShim, "with -secure-boot-dir: the file in that folder that PCs are told to boot (a distribution's shim asks for grubx64.efi next; the iPXE project's ipxe-shimx64.efi asks for ipxe.efi)")
	sbAll := fs.Bool("secure-boot-all", false, "with -secure-boot-dir: send every 64-bit UEFI PC through the shim, not only the PCs marked in the web interface")
	debugSMB := fs.Bool("debug-smb", false, "log every message of the Windows file share (a lot: for finding faults)")
	if err := applyEnv(fs, osLookup); err != nil { // (before the command line, so that the command line wins)
		fmt.Fprintf(stderr, "bootlace: %v\n", err)
		return 2
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 { // flag parsing stops at the first stray word and would ignore every option after it, leaving a server with settings nobody chose
		fmt.Fprintf(stderr, "serve: unexpected argument %q (a folder name with a space must be in quotes); nothing was started\n", fs.Arg(0))
		return 2
	}
	history := logbuf.New(2000) // the last lines of the log, for the web interface
	logf := func(format string, a ...any) {
		fmt.Fprintf(stdout, time.Now().Format("15:04:05")+"  "+format+"\n", a...)
		history.Logf(format, a...)
	}
	if *logFile != "" {
		if err := history.SetFile(*logFile, int64(*logSize)<<20); err != nil {
			fmt.Fprintf(stderr, "bootlace: cannot write the log file: %v\n", err)
			return 1
		}
		defer history.Close()
	}
	ip := net.ParseIP(*ipFlag)
	if ip == nil {
		ip = privateIP()
	}
	if ip == nil {
		fmt.Fprintln(stderr, "bootlace: cannot find this computer's address: give it with -ip")
		return 1
	}
	base := fmt.Sprintf("http://%s:%d", ip, *httpPort)
	registry := &handoff.Registry{}
	seed := config.Config{MenuTimeout: *menuTimeout, PCDays: config.DefaultPCDays, Hidden: map[string]bool{}, Titles: map[string]string{}}
	seed.Default = "local" // nothing chosen after the timeout: the PC goes on to its own disk, so that a PC started unattended does not sit at the menu
	switch {
	case *menuPick != "":
		seed.Default = "pick:" + *menuPick
	case *menuLocal:
		seed.Default = "local"
	case *menuDefault > 0:
		seed.Default = fmt.Sprintf("index:%d", *menuDefault)
	}
	store, created, err := config.Open(*configPath, seed)
	if err != nil {
		fmt.Fprintf(stderr, "bootlace: cannot use the settings file: %v\n", err)
		return 1
	}
	if created {
		logf("settings file %s made from the command-line menu options", store.Path())
	}
	// the share's password is inside the copies of boot.wim that are made for Secure Boot, so it is kept: a new one at every start would make them all again
	smbPassword := *smbPasswordFlag
	if smbPassword == "" {
		smbPassword = store.Get().SMBPassword
	}
	if smbPassword == "" {
		smbPassword = randomPassword()
		if err := store.Update(func(c *config.Config) error { c.SMBPassword = smbPassword; return nil }); err != nil {
			logf("cannot keep the share's password in the settings file: %v", err)
		}
	}
	policy := access.New(logf)
	if a := store.Get().Access; policy.Set(a.Networks, a.AllowPublic) != nil {
		logf("the saved list of allowed networks is not valid: only the local network is answered")
	}
	if err := access.ServerAddress(ip); err != nil {
		logf("warning: %v", err)
	}
	lib := openLibrary(store, *isoDir, logf)
	sup := &service.Supervisor{Gate: lib.Check, Grace: 30 * time.Second, Logf: logf}
	indexer := &library.Indexer{Lib: lib, Idle: func() bool { return len(sup.Sessions()) == 0 }}
	imp := importer.New(lib, indexer.Get, logf)
	imp.Installing = func() bool { return len(sup.Sessions()) > 0 }
	imp.Limit = func() int64 { return int64(store.Get().ImportLimitMBps) << 20 }
	background := make(chan struct{}) // ends the hashing and the imports when the program stops
	go indexer.Run(background)
	go imp.Run(background)
	dataDir := filepath.Dir(*configPath)
	web := &httpd.Server{CheckPassword: admin.CheckPassword, CacheSettings: func() (string, int64) { c := store.Get().Cache; return c.Where, int64(c.LocalLimitMB) << 20 }, Handoff: registry, TFTPAhead: *tftpAhead, SMBPort: *smbPort, SMBPassword: smbPassword, Library: lib, Active: sup.Active, Access: policy, Base: base, Logf: logf, Config: store, PCFile: filepath.Join(dataDir, "pcs.json"),
		Wimlib: *wimlibFlag, CacheDir: filepath.Join(dataDir, "cache"), NBDPort: *nbdPort, ISCSIPort: *iscsiPort,
		SecureBoot: httpd.SecureBoot{Dir: *sbDir, Shim: *sbShim, All: *sbAll}}
	if err := web.SecureBoot.Check(); err != nil {
		fmt.Fprintf(stderr, "bootlace: -secure-boot-dir: %v\n", err)
		return 2
	}
	if *sbDir != "" {
		logf("Secure Boot route (prototype): PCs marked for it%s are sent to the shim in %s, which starts iPXE signed with your key", map[bool]string{true: " and every 64-bit UEFI PC", false: ""}[*sbAll], *sbDir)
	}
	tidier := &tidy.Tidier{Library: lib, Index: indexer.Get, Caches: web.CacheDirs, Logf: logf,
		ISOIDs: func() (map[string]bool, error) {
			list, err := isos.Scan(lib.Path())
			ids := map[string]bool{}
			for _, i := range list {
				ids[i.ID] = true
			}
			return ids, err
		},
		Owned: func() map[string]bool {
			owned := map[string]bool{}
			for _, j := range imp.Jobs() {
				if j.Part != "" {
					owned[filepath.Base(j.Part)] = true
				}
			}
			for part := range imp.UploadParts() { // an upload in pieces that is still open
				owned[part] = true
			}
			return owned
		},
		Expire: imp.ExpireUploads}
	go tidier.Loop(background)
	if winpe.Wimlib(*wimlibFlag) == "" {
		logf("wimlib-imagex was not found: Windows cannot be started with UEFI Secure Boot on (install wimtools)")
	}
	if err := web.LoadClients(); err != nil {
		logf("cannot read the saved list of PCs (%v): starting with an empty list", err)
	}
	settingsCopy := &backup.Sync{Store: store, Lib: lib, Version: version.Version, Logf: logf} // the copy of the settings in the library
	store.OnChange(settingsCopy.Changed)
	go func() {
		for { // now and every little while: the library may be chosen later, or come back
			settingsCopy.Check()
			select {
			case <-background:
				return
			case <-time.After(30 * time.Second):
			}
		}
	}()
	pxeChecker := &pxecheck.Checker{Logf: logf, Options: func() pxecheck.Options {
		return pxecheck.Options{Local: ip, Mask: maskOf(ip), Full: *mode == "full"}
	}}
	if *mode != "off" { // once at start, when the services have had time to come up
		go func() {
			select {
			case <-background:
				return
			case <-time.After(10 * time.Second):
			}
			_, _ = pxeChecker.Run(context.Background())
		}()
	}
	if !*noAdmin {
		generated, err := admin.EnsurePassword(store, *adminPassword)
		if err != nil {
			fmt.Fprintf(stderr, "bootlace: cannot set the web interface password: %v\n", err)
			return 1
		}
		if generated != "" {
			// shown on the console and in the web log, never written to the log file
			line := fmt.Sprintf("web interface: the password was made up: %s  (change it under Account; it is shown only this once)", generated)
			fmt.Fprintln(stdout, time.Now().Format("15:04:05")+"  "+line)
			history.AddPrivate(line)
		}
		facts := [][2]string{{"Web interface", fmt.Sprintf("%s/admin/", base)}, {"Boot menu", base + "/boot.ipxe"}, {"Server address", ip.String()}, {"DHCP", *mode},
			{"Windows file share", fmt.Sprintf(`\\%s\%s (port %d)`, ip, smbshare.ShareName, *smbPort)}}
		web.Mount = admin.New(admin.Options{Store: store, Log: history, Backend: web, Facts: facts, Library: lib, Services: sup, Access: policy, ServerIP: ip.String(), Importer: imp, Indexer: indexer, Logf: logf,
			PXE: pxeChecker, Sync: settingsCopy, SecureBoot: secureBootMode(*sbDir, *sbAll),
			Wake: func(mac net.HardwareAddr) error {
				return wol.Send(mac, ip, net.JoinHostPort(wol.Broadcast(ip, maskOf(ip)).String(), "9"), "255.255.255.255:9")
			}, Version: version.Version, SelfTest: func() selftest.Target {
				t := selftest.Target{IP: ip.String(), HTTP: *httpPort, SMB: *smbPort, NBD: *nbdPort, ISCSI: *iscsiPort}
				if infos, err := web.ISOInfos(); err == nil && len(infos) > 0 {
					t.ISOPath = httpd.ISOPath(infos[0].Name)
				}
				return t
			}}).Mount()
		logf("web interface: %s/admin/", base)
	}
	httpListener, err := net.Listen("tcp", fmt.Sprintf(":%d", *httpPort)) // both IPv4 and IPv6: the addresses given to PCs are still IPv4 (issue #29, stage 1)
	if err != nil {
		fmt.Fprintf(stderr, "bootlace: cannot open web port %d: %v\n", *httpPort, err)
		return 1
	}
	webErr := make(chan error, 1)
	go func() { webErr <- http.Serve(httpListener, web.Handler()) }()

	responder, err := dhcpResponder(*mode, ip, base, registry, web, dhcpOptions{first: *first, last: *last, mask: *mask, router: *router, dns: *dnsIP, pxeOnly: *pxeOnly}, logf)
	if err != nil {
		fmt.Fprintf(stderr, "bootlace: %v\n", err)
		return 2
	}
	addServices(sup, serverPlan{ip: ip, web: web, lib: lib, httpPort: *httpPort, smbPort: *smbPort, nbdPort: *nbdPort, iscsiPort: *iscsiPort,
		smbUser: "bootlace", smbPassword: smbPassword, smbForward: *smbForward, debugSMB: *debugSMB, dhcp: responder, allow: policy.AllowedAddr, logf: logf})
	go func() { // follows the library: no library, no server
		for range time.Tick(5 * time.Second) {
			sup.Reconcile()
		}
	}()
	logf("Gola Bootlace %s: menu at %s/boot.ipxe, library %q, DHCP mode %s", version.Version, base, lib.Path(), *mode)
	if store.Get().NoAutoStart {
		logf("the services are stopped (start them in the web interface)")
	} else if err := sup.Start(); err != nil {
		logf("the services wait: %v", err)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-webErr:
		close(background)
		sup.Stop()
		fmt.Fprintf(stderr, "bootlace: %v\n", err)
		return 1
	case <-stop:
		logf("stopping")
		close(background)
		time.Sleep(time.Second) // (an import that was running notes that it was interrupted, so that it can go on at the next start)
		sup.Stop()
		return 0
	}
}

// secureBootMode is how the signed-iPXE route is offered, for the web interface: "" (off), "pcs" or "all".
func secureBootMode(dir string, all bool) string {
	switch {
	case dir == "":
		return ""
	case all:
		return "all"
	}
	return "pcs"
}

// openLibrary is the library saved in the settings. On the very first start, a folder given on the command line is added as the library: giving it is the person's
// own act, and it is the only time a command line adds one.
func openLibrary(store *config.Store, flagDir string, logf func(string, ...any)) *library.Library {
	saved := store.Get().Library
	lib := library.New(saved.Path, saved.ID)
	if flagDir == "" {
		return lib
	}
	dir, err := library.Clean(flagDir)
	if err != nil {
		logf("-iso-dir %q cannot be the library: %v", flagDir, err)
		return lib
	}
	if saved.Path != "" {
		if dir != saved.Path {
			logf("-iso-dir %q is not used: the library is %q (change it in the web interface)", flagDir, saved.Path)
		}
		return lib
	}
	id, err := library.Prepare(dir)
	if err != nil {
		logf("-iso-dir %q cannot be the library: %v", flagDir, err)
		return lib
	}
	if err := store.Update(func(c *config.Config) error { c.Library = config.Library{Path: dir, ID: id}; return nil }); err != nil {
		logf("cannot keep the library in the settings file: %v", err)
		return lib
	}
	lib.Set(dir, id)
	logf("library: %s (added from -iso-dir)", dir)
	return lib
}

// dhcpOptions are the settings of the DHCP server in full mode.
type dhcpOptions struct {
	first, last, mask, router, dns string
	pxeOnly                        bool
}

// dhcpResponder is what answers PCs that ask for a network boot: nil when DHCP is off. A setting that is wrong is an error here, at the start of the program,
// not later when the services are started.
func dhcpResponder(mode string, ip net.IP, base string, registry *handoff.Registry, web *httpd.Server, o dhcpOptions, logf func(string, ...any)) (dhcp.Responder, error) {
	switch mode {
	case "off":
		return nil, nil
	case "proxy", "full":
	default:
		return nil, fmt.Errorf("-dhcp must be proxy, full or off, not %q", mode)
	}
	handoffFile := func(mac string, efi bool) (string, bool) { // a hand-off in progress first, then a rule that wants the firmware's own boot manager
		if f, ok := registry.BootFile(mac, efi); ok {
			return f, true
		}
		return web.NativeBootFile(mac, efi)
	}
	boot := dhcp.Boot{ServerIP: ip, HTTPBase: base, Script: base + "/boot.ipxe", Handoff: handoffFile, Shim: web.SecureBootFile, Seen: web.NoteDHCP, LoaderAddress: registry.LoaderAddress, Mask: maskOf(ip), Files: map[dhcp.Arch]string{
		dhcp.ArchBIOS: "undionly.kpxe", dhcp.ArchX64EFI: "ipxe.efi",
	}}
	if mode == "proxy" {
		return boot, nil
	}
	m := net.IPMask(net.ParseIP(o.mask).To4())
	if m == nil {
		return nil, fmt.Errorf("%q is not a network mask", o.mask)
	}
	start, end := net.ParseIP(o.first), net.ParseIP(o.last)
	if start == nil || end == nil {
		network := ip.Mask(m).To4()
		start, end = net.IPv4(network[0], network[1], network[2], 100), net.IPv4(network[0], network[1], network[2], 200)
	}
	logf("DHCP: handing out %s to %s", start, end)
	return &dhcp.Full{PXEOnly: o.pxeOnly, Boot: boot, First: start, Last: end, Mask: m, Router: net.ParseIP(o.router), DNS: net.ParseIP(o.dns), Lease: time.Hour}, nil
}

// randomPassword is what Windows set-up logs in to the share with: new for every start of the server.
func randomPassword() string {
	b := make([]byte, 9)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// privateIP is this computer's private IPv4 address on the network the PCs are on. The address that the computer uses to reach the rest of the network is the
// one (a computer, and especially a container, often has several: a bridge made by Docker is private too, and is the wrong one). Only when that is not a private
// address, the first private address of a real network card is taken.
func privateIP() net.IP {
	if c, err := net.Dial("udp4", "192.0.2.1:9"); err == nil { // (no packet is sent: the system only says which address it would use)
		ip := c.LocalAddr().(*net.UDPAddr).IP.To4()
		c.Close()
		if ip != nil && ip.IsPrivate() {
			return ip
		}
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 || virtualInterface(ifc.Name) {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && n.IP.IsPrivate() {
				return n.IP.To4()
			}
		}
	}
	return nil
}

// virtualInterface says whether a network card's name is one that a container system or a virtual machine host makes (not a network PCs are on).
func virtualInterface(name string) bool {
	for _, p := range []string{"docker", "br-", "veth", "virbr", "cni", "flannel", "cali", "lxcbr", "lxdbr", "podman", "vmnet", "vEthernet"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// maskOf is the network mask of the network card that has this address (24 bits when it is not found).
func maskOf(ip net.IP) net.IPMask {
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.Equal(ip) && len(n.Mask) > 0 {
			if m := n.Mask; len(m) == 16 {
				return m[12:]
			} else {
				return m
			}
		}
	}
	return net.CIDRMask(24, 32)
}
