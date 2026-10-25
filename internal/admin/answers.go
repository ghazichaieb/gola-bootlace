// The library of answer files for unattended installs (issue #17): list, create, change and delete, shown with the secrets masked unless asked for. Nothing here writes a file's
// text to the log.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package admin

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sort"
	"strings"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
)

// answerTemplates are starting points, one per kind. Each one erases a disk: the first lines of every file say so, and the passwords in them have to be changed.
var answerTemplates = map[string]string{
	"preseed": `# Debian preseed: an unattended install. IT ERASES THE DISK of every PC that starts with it. Put a password HASH in the line below first (mkpasswd -m sha-512):
# anyone on the network can read this file, so never type a plain password into it. Until then nobody can sign in with a password.
# The words hostname, name, mac, mac-dash, ip and serial between double curly braces are replaced for the PC that asks (hostname: the name given on the PCs page, or pc- and the end of its address).
d-i debian-installer/locale string en_US.UTF-8
d-i keyboard-configuration/xkb-keymap select us
d-i netcfg/choose_interface select auto
d-i netcfg/get_hostname string {{hostname}}
d-i netcfg/get_domain string lan
d-i netcfg/hostname string {{hostname}}
d-i mirror/country string manual
d-i mirror/http/hostname string deb.debian.org
d-i mirror/http/directory string /debian
d-i mirror/http/proxy string
d-i passwd/root-login boolean false
d-i passwd/user-fullname string Administrator
d-i passwd/username string localadmin
d-i passwd/user-password-crypted password PUT-A-PASSWORD-HASH-HERE
d-i clock-setup/utc boolean true
d-i time/zone string UTC
d-i partman-auto/method string regular
d-i partman-auto/choose_recipe select atomic
d-i partman-partitioning/confirm_write_new_label boolean true
d-i partman/choose_partition select finish
d-i partman/confirm boolean true
d-i partman/confirm_nooverwrite boolean true
tasksel tasksel/first multiselect standard
d-i pkgsel/include string openssh-server
d-i popularity-contest/participate boolean false
d-i grub-installer/only_debian boolean true
d-i grub-installer/bootdev string default
d-i finish-install/reboot_in_progress note
`,
	"autoinstall": `#cloud-config
# Ubuntu Server autoinstall. IT ERASES THE DISK of every PC that starts with it. Put a crypted password in "password" first (openssl passwd -6); anyone on the network can read this file,
# so never type a plain password into it.
# The words hostname, name, mac, mac-dash, ip and serial between double curly braces are replaced for the PC that asks (hostname: the name given on the PCs page, or pc- and the end of its address).
autoinstall:
  version: 1
  locale: en_US.UTF-8
  keyboard:
    layout: us
  identity:
    hostname: {{hostname}}
    username: ubuntu
    password: "PUT-A-CRYPTED-PASSWORD-HERE"
  ssh:
    install-server: true
    allow-pw: true
  storage:
    layout:
      name: direct
  shutdown: reboot
`,
	"kickstart": `# Red Hat kickstart (Fedora, Alma, Rocky, CentOS). IT ERASES THE DISKS of every PC that starts with it. Put a password HASH in the rootpw line first (openssl passwd -6): anyone on the network
# can read this file, so never type a plain password into it. Until then nobody can sign in as root with a password.
# The words hostname, name, mac, mac-dash, ip and serial between double curly braces are replaced for the PC that asks (hostname: the name given on the PCs page, or pc- and the end of its address).
# Where the packages come from is already on the kernel line (inst.repo).
text
lang en_US.UTF-8
keyboard us
timezone UTC
network --bootproto=dhcp --hostname={{hostname}}
rootpw --iscrypted PUT-A-PASSWORD-HASH-HERE
zerombr
clearpart --all --initlabel
autopart
bootloader --location=mbr
reboot
%packages
@^minimal-environment
%end
`,
	"autoyast": `<?xml version="1.0"?>
<!DOCTYPE profile>
<!-- openSUSE AutoYaST. IT ERASES THE DISKS of every PC that starts with it. Put a password HASH in user_password first (openssl passwd -6): anyone on the network can read this file,
     so never type a plain password into it.
     The words hostname, name, mac, mac-dash, ip and serial between double curly braces are replaced for the PC that asks (hostname: the name given on the PCs page, or pc- and the end of its address). -->
<profile xmlns="http://www.suse.com/1.0/yast2ns" xmlns:config="http://www.suse.com/1.0/configns">
  <general>
    <mode>
      <confirm config:type="boolean">false</confirm>
    </mode>
  </general>
  <networking>
    <dns>
      <hostname>{{hostname}}</hostname>
    </dns>
  </networking>
  <users config:type="list">
    <user>
      <username>root</username>
      <user_password>PUT-A-PASSWORD-HASH-HERE</user_password>
      <encrypted config:type="boolean">true</encrypted>
    </user>
  </users>
  <partitioning config:type="list">
    <drive>
      <use>all</use>
    </drive>
  </partitioning>
</profile>
`,
}

func (a *Admin) answerTemplates(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"kinds": config.AnswerKinds, "templates": answerTemplates})
}

// answersList is the library without the texts: what each file is, and what it is attached to.
func (a *Admin) answersList(w http.ResponseWriter, _ *http.Request) {
	c := a.o.Store.Get()
	titles := map[string]string{}
	if infos, err := a.o.Backend.ISOInfos(); err == nil {
		for _, i := range infos {
			titles[i.ID] = i.Title
		}
	}
	type used struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	type entry struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Kind string `json:"kind"`
		Size int    `json:"size"`
		ISOs []used `json:"isos"`
		PCs  []used `json:"pcs"`
	}
	list := []entry{}
	for id, f := range c.Answers {
		e := entry{ID: id, Name: f.Name, Kind: f.Kind, Size: len(f.Content), ISOs: []used{}, PCs: []used{}}
		for iso, aid := range c.ISOAnswers {
			if aid == id {
				title := titles[iso]
				if title == "" {
					title = iso
				}
				e.ISOs = append(e.ISOs, used{iso, title})
			}
		}
		for mac, o := range c.PCOptions {
			if o.Answer == id {
				name := c.PCNames[mac]
				if name == "" {
					name = mac
				}
				e.PCs = append(e.PCs, used{mac, name})
			}
		}
		sort.Slice(e.ISOs, func(i, j int) bool { return e.ISOs[i].Title < e.ISOs[j].Title })
		sort.Slice(e.PCs, func(i, j int) bool { return e.PCs[i].Title < e.PCs[j].Title })
		list = append(list, e)
	}
	sort.Slice(list, func(i, j int) bool { return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name) })
	writeJSON(w, http.StatusOK, map[string]any{"kinds": config.AnswerKinds, "answers": list, "pcAnswers": pcAnswerChoices(c)})
}

// pcAnswerChoices is the answer file choice of each PC that has one (a MAC address and the id, or "none"), for the PCs page.
func pcAnswerChoices(c config.Config) map[string]string {
	out := map[string]string{}
	for mac, o := range c.PCOptions {
		if o.Answer != "" {
			out[mac] = o.Answer
		}
	}
	return out
}

// answerGet is one file. The values that look like secrets are masked unless reveal=1 is given.
func (a *Admin) answerGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	f, ok := a.o.Store.Get().Answers[id]
	if !ok {
		fail(w, http.StatusNotFound, "no such answer file")
		return
	}
	reveal := r.URL.Query().Get("reveal") == "1"
	text := f.Content
	if !reveal {
		text = config.MaskAnswer(f.Content)
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "name": f.Name, "kind": f.Kind, "content": text, "masked": !reveal && text != f.Content})
}

func newAnswerID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (a *Admin) answerCreate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name    string `json:"name"`
		Kind    string `json:"kind"`
		Content string `json:"content"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	id, err := newAnswerID()
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	err = a.o.Store.Update(func(c *config.Config) error {
		if c.Answers == nil {
			c.Answers = map[string]config.AnswerFile{}
		}
		c.Answers[id] = config.AnswerFile{Name: in.Name, Kind: in.Kind, Content: in.Content} // (Validate checks the name, the kind and the text)
		return nil
	})
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if a.o.Logf != nil {
		a.o.Logf("web interface: answer file %q (%s) created", strings.TrimSpace(in.Name), in.Kind)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
}

// answerUpdate changes the name, the kind or the text. A text that was shown masked and saved as it is keeps the old values; a line that still shows the mask after that is refused,
// because the real value would be lost.
func (a *Admin) answerUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var in struct {
		Name    *string `json:"name"`
		Kind    *string `json:"kind"`
		Content *string `json:"content"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	name := ""
	err := a.o.Store.Update(func(c *config.Config) error {
		f, ok := c.Answers[id]
		if !ok {
			return errNoAnswer
		}
		if in.Name != nil {
			f.Name = *in.Name
		}
		if in.Kind != nil {
			f.Kind = *in.Kind
		}
		if in.Content != nil {
			text := config.RestoreMasked(f.Content, *in.Content)
			if strings.Contains(text, config.Masked) && !strings.Contains(f.Content, config.Masked) {
				return errMaskedLine
			}
			f.Content = text
		}
		name = f.Name
		c.Answers[id] = f
		return nil
	})
	switch err {
	case nil:
	case errNoAnswer:
		fail(w, http.StatusNotFound, err.Error())
		return
	default:
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if a.o.Logf != nil {
		a.o.Logf("web interface: answer file %q changed", name)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Admin) answerDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	name := ""
	err := a.o.Store.Update(func(c *config.Config) error {
		f, ok := c.Answers[id]
		if !ok {
			return errNoAnswer
		}
		name = f.Name
		delete(c.Answers, id) // (what was attached to it is dropped by the check)
		return nil
	})
	if err == errNoAnswer {
		fail(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if a.o.Logf != nil {
		a.o.Logf("web interface: answer file %q deleted", name)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type answerError string

func (e answerError) Error() string { return string(e) }

const (
	errNoAnswer   = answerError("no such answer file")
	errMaskedLine = answerError("a line still shows ******** in place of a value that is not in the file: type the real value, or leave the line as it was shown")
)

// pcAnswer sets the answer file one PC gets: a file's id, "none" to keep every file away from it although its ISO has one, or empty to follow the ISO. Nothing else of the PC's
// options changes.
func (a *Admin) pcAnswer(w http.ResponseWriter, r *http.Request) {
	mac, ok := config.NormalizeMAC(r.PathValue("mac"))
	if !ok {
		fail(w, http.StatusBadRequest, "that is not a MAC address (like aa:bb:cc:dd:ee:ff)")
		return
	}
	var in struct {
		Answer string `json:"answer"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	err := a.o.Store.Update(func(c *config.Config) error {
		if in.Answer != "" && in.Answer != config.NoAnswer {
			if _, ok := c.Answers[in.Answer]; !ok {
				return errNoAnswer
			}
		}
		if c.PCOptions == nil {
			c.PCOptions = map[string]config.PCOptions{}
		}
		o := c.PCOptions[mac]
		o.Answer = in.Answer
		c.PCOptions[mac] = o // (an entry with nothing set is dropped by the check)
		return nil
	})
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if a.o.Logf != nil {
		a.o.Logf("web interface: answer file of %s: %s", mac, in.Answer)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
