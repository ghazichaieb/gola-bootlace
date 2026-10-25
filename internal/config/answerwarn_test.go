package config

import (
	"strings"
	"testing"
)

func lines(ps []PlainPassword) string {
	var out []string
	for _, p := range ps {
		out = append(out, p.String())
	}
	return strings.Join(out, "; ")
}

func TestPlainPasswordsAreFoundInTheFormOfEachKind(t *testing.T) {
	for _, c := range []struct {
		kind, text, want string
	}{
		{"preseed", "d-i passwd/user-password password hunter2\nd-i passwd/user-password-again password hunter2\nd-i passwd/root-password string x", "line 1: d-i passwd/user-password; line 2: d-i passwd/user-password-again; line 3: d-i passwd/root-password"},
		{"preseed", "d-i passwd/user-password-crypted password $6$salt$abc\nd-i passwd/user-password-crypted password PUT-A-PASSWORD-HASH-HERE\nd-i passwd/root-login boolean false\n# d-i passwd/user-password password x", ""},
		{"preseed", "d-i partman-crypto/passphrase password s3cret", "line 1: d-i partman-crypto/passphrase"},
		{"autoinstall", "autoinstall:\n  identity:\n    username: u\n    password: \"plain\"\n", "line 4: password"},
		{"autoinstall", "autoinstall:\n  identity:\n    password: \"$6$rounds=4096$abc$def\"\n  user-data:\n    users:\n      - passwd: $6$x$y\n", ""},
		{"autoinstall", "    password: \"PUT-A-CRYPTED-PASSWORD-HERE\"\n    password: {{hostname}}\n    # password: plain\n", ""},
		{"autoinstall", "users:\n  - name: a\n    plain_text_passwd: hunter2\nchpasswd:\n  list:\n    - root:hunter2\n    - bob:$6$a$b\n", "line 3: plain_text_passwd; line 6: chpasswd list"},
		{"kickstart", "rootpw hunter2\n", "line 1: rootpw"},
		{"kickstart", "rootpw --plaintext hunter2\n", "line 1: rootpw"},
		{"kickstart", "rootpw --iscrypted $6$abc$def\nrootpw --iscrypted PUT-A-PASSWORD-HASH-HERE\nrootpw --lock\n# rootpw plain\n", ""},
		{"kickstart", "user --name=bob --password=hunter2\nuser --name=al --password=$6$a$b --iscrypted\n", "line 1: user --password"},
		{"autoyast", "<user>\n<username>root</username>\n<user_password>hunter2</user_password>\n</user>\n", "line 3: user_password"},
		{"autoyast", "<user>\n<user_password>$6$a$b</user_password>\n</user>\n<user>\n<user_password>cleartext</user_password>\n<encrypted config:type=\"boolean\">true</encrypted>\n</user>\n", ""},
		{"autoyast", "<!-- <user_password>hunter2</user_password> -->\n<user>\n<user_password>PUT-A-PASSWORD-HASH-HERE</user_password>\n</user>", ""},
		{"nonsense", "password: x", ""},
	} {
		if got := lines(PlainPasswords(c.kind, c.text)); got != c.want {
			t.Errorf("%s %q:\n got %q\nwant %q", c.kind, c.text, got, c.want)
		}
	}
}

func TestTheShippedTemplatesHoldNoPlainPassword(t *testing.T) {
	// (the templates live in package admin; this checks the shapes they use)
	for kind, text := range map[string]string{
		"preseed":     "d-i passwd/user-password-crypted password PUT-A-PASSWORD-HASH-HERE",
		"autoinstall": "    password: \"PUT-A-CRYPTED-PASSWORD-HERE\"",
		"kickstart":   "rootpw --iscrypted PUT-A-PASSWORD-HASH-HERE",
		"autoyast":    "<user>\n<user_password>PUT-A-PASSWORD-HASH-HERE</user_password>\n<encrypted config:type=\"boolean\">true</encrypted>\n</user>",
	} {
		if got := PlainPasswords(kind, text); len(got) != 0 {
			t.Errorf("%s: %v", kind, got)
		}
	}
}

const unattendText = `<settings>
  <!-- <Password><Value>commented</Value></Password> -->
  <Password>
    <Value>c2VjcmV0</Value>
    <PlainText>false</PlainText>
  </Password>
  <MetaData><Key>/IMAGE/NAME</Key><Value>Windows 11 Pro</Value></MetaData>
  <AdministratorPassword><Value>{{serial}}</Value></AdministratorPassword>
  <Password><Value>  </Value></Password>
  <Password><Value>one-line</Value></Password>
</settings>
`

func TestAWindowsAnswerFileWithAPasswordIsWarnedAboutAndMaskedAndTheMaskIsRestoredOnSave(t *testing.T) {
	if got := lines(PlainPasswords("unattend", unattendText)); got != "line 4: Password Value (Windows keeps it as base64, not as a hash); line 10: Password Value (Windows keeps it as base64, not as a hash)" {
		t.Fatalf("%s", got)
	}
	masked := MaskAnswer(unattendText)
	if strings.Contains(masked, "c2VjcmV0") || strings.Contains(masked, "one-line") || !strings.Contains(masked, "<Value>********</Value>") {
		t.Fatalf("%s", masked)
	}
	if !strings.Contains(masked, "<Value>Windows 11 Pro</Value>") || !strings.Contains(masked, "commented") || !strings.Contains(masked, "{{serial}}") {
		t.Fatalf("what is not a password stays:\n%s", masked)
	}
	// saving the masked text with another edit keeps the passwords
	edited := strings.Replace(masked, "<PlainText>false", "<PlainText>true", 1)
	back := RestoreMasked(unattendText, edited)
	if want := strings.Replace(unattendText, "<PlainText>false", "<PlainText>true", 1); back != want {
		t.Fatalf("%s\nwant\n%s", back, want)
	}
	// CR LF files
	crlf := strings.ReplaceAll(unattendText, "\n", "\r\n")
	if m := MaskAnswer(crlf); strings.Contains(m, "c2VjcmV0") || !strings.Contains(m, "\r\n") {
		t.Fatalf("%q", m)
	}
}
