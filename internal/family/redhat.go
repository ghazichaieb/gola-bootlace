// Systems whose start-up disk is made with dracut: Fedora and the ones built from it (Nobara, Bazzite), Rocky, Alma and CentOS, Gentoo's live ISOs; and the installers of
// that family and openSUSE's, which only need the packages' address.
//
// A dracut live system can be told where its ISO is: as a network block disk (netroot=nbd, which the extra archive's handler attaches) or as an iSCSI CD that dracut's own
// client logs in to; a live system that has neither gets the whole ISO downloaded.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package family

// dracutLive is how a live system made with dracut is told to find its ISO over the network without downloading it. extra is added to the arguments (what the system's own
// boot menu passes besides).
func (c *Context) dracutLive(family string, f files, extra string) Boot {
	u := c.URLs
	switch u.pick(NBD, ISCSI, Memory) {
	case NBD:
		args := "ip=dhcp rd.neednet=1 netroot=nbd:" + u.NBDHost + ":" + u.nbdPort() + ":" + u.NBDName + " root=live:/dev/nbd0 rd.live.image console=tty0" + extra
		return newBoot(family, f, args).withExtra(u, nil)
	case ISCSI:
		return newBoot(family, f, dracutISCSI(u)+" rd.live.image console=tty0"+extra)
	}
	return newBoot(family, f, "ip=dhcp root=live:"+u.ISO+" rd.live.image console=tty0"+extra)
}

// dracutISCSI is how a dracut live system is told to find its ISO as a network CD-ROM: it logs in over iSCSI and looks for the disk by its path.
func dracutISCSI(u URLs) string {
	port := itoa(u.ISCSIPort)
	return "ip=dhcp rd.neednet=1 netroot=iscsi:" + u.ISCSIHost + "::" + port + "::" + u.ISCSIIQN +
		" root=live:/dev/disk/by-path/ip-" + u.ISCSIHost + ":" + port + "-iscsi-" + u.ISCSIIQN + "-lun-0"
}

// Fedora's live ISOs since version 44 (built with KIWI): kernel and initramfs in /boot/x86_64/loader, the system in /LiveOS/squashfs.img.
func fedoraLiveISO(c *Context) (Boot, bool) {
	const dir = "/boot/x86_64/loader"
	t := c.FS
	if !t.Exists(dir+"/linux") || !t.Exists(dir+"/initrd") || !t.Exists("/LiveOS/squashfs.img") || t.Exists("/.treeinfo") {
		return Boot{}, false
	}
	return c.dracutLive("Fedora (live)", c.in(dir+"/linux", dir+"/initrd"), ""), true
}

// Fedora, CentOS, Rocky, Alma and other Red Hat family ISOs: an installer reads its packages from the web server, a live ISO is read from the network disk.
func fedoraLike(c *Context) (Boot, bool) {
	t := c.FS
	if !t.Exists("/images/pxeboot/vmlinuz") || !t.Exists("/images/pxeboot/initrd.img") {
		return Boot{}, false
	}
	f := c.in("/images/pxeboot/vmlinuz", "/images/pxeboot/initrd.img")
	if t.Exists("/LiveOS") {
		// (Bazzite's own boot menu passes enforcing=0: with SELinux enforcing its live system stops at the first thing it labels. A live session loses nothing by it.)
		return c.dracutLive("Fedora (live)", f, " enforcing=0"), true
	}
	return newBoot("Red Hat family (installer)", f, "ip=dhcp inst.repo="+c.URLs.FS+" console=tty0"), true
}

// Gentoo's live ISOs (minimal install CD, LiveGUI): a dracut live system with its kernel and initramfs in /boot and the system in /image.squashfs.
func gentooLive(c *Context) (Boot, bool) {
	t := c.FS
	if !t.Exists("/boot/gentoo") || !t.Exists("/boot/gentoo.igz") || !t.Exists("/image.squashfs") {
		return Boot{}, false
	}
	return c.dracutLive("Gentoo (live)", c.in("/boot/gentoo", "/boot/gentoo.igz"), " rd.live.dir=/ rd.live.squashimg=image.squashfs cdroot"), true
}

// openSUSE (Leap, Tumbleweed): the installer reads the packages from the web server, file by file, when the ISO has them (the DVD); the network ISO asks the person.
func openSUSE(c *Context) (Boot, bool) {
	const dir = "/boot/x86_64/loader"
	t := c.FS
	if !t.Exists(dir+"/linux") || !t.Exists(dir+"/initrd") || !t.Exists("/.treeinfo") {
		return Boot{}, false
	}
	args := "netsetup=dhcp"
	if t.Exists("/repodata") {
		args += " install=" + c.URLs.FS + "/"
	}
	return newBoot("openSUSE", c.in(dir+"/linux", dir+"/initrd"), args), true
}
