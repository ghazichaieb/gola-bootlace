package config

import (
	"strings"
	"testing"
)

const id1, id2 = "0123456789ab", "ba9876543210"

func TestAnswerFilesAreChecked(t *testing.T) {
	good := func() Config {
		c := Config{Answers: map[string]AnswerFile{id1: {Name: "Debian lab", Kind: "preseed", Content: "d-i debian-installer/locale string en_US\n"}}}
		return c
	}
	c := good()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Config){
		"unknown kind":   func(c *Config) { c.Answers[id1] = AnswerFile{Name: "x", Kind: "winxp", Content: "x"} },
		"empty name":     func(c *Config) { c.Answers[id1] = AnswerFile{Name: " ", Kind: "preseed", Content: "x"} },
		"two-line name":  func(c *Config) { c.Answers[id1] = AnswerFile{Name: "a\nb", Kind: "preseed", Content: "x"} },
		"NUL in content": func(c *Config) { c.Answers[id1] = AnswerFile{Name: "x", Kind: "preseed", Content: "a\x00b"} },
		"too big": func(c *Config) {
			c.Answers[id1] = AnswerFile{Name: "x", Kind: "preseed", Content: strings.Repeat("a", MaxAnswerBytes+1)}
		},
		"bad id": func(c *Config) { c.Answers["../etc"] = AnswerFile{Name: "x", Kind: "preseed", Content: "x"} },
	} {
		c := good()
		mutate(&c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestAChoiceThatNamesAFileThatIsGoneIsDropped(t *testing.T) {
	c := Config{
		Answers:    map[string]AnswerFile{id1: {Name: "x", Kind: "kickstart", Content: "x"}},
		ISOAnswers: map[string]string{"abc1234567": id1, "def4567890": id2},
		PCOptions:  map[string]PCOptions{"aa:bb:cc:dd:ee:01": {Answer: id2}, "aa:bb:cc:dd:ee:02": {Answer: id1}, "aa:bb:cc:dd:ee:03": {Answer: NoAnswer}},
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(c.ISOAnswers) != 1 || c.ISOAnswers["abc1234567"] != id1 {
		t.Errorf("ISO choices: %v", c.ISOAnswers)
	}
	if _, kept := c.PCOptions["aa:bb:cc:dd:ee:01"]; kept {
		t.Errorf("a PC whose only setting named a file that is gone keeps nothing: %v", c.PCOptions)
	}
	if c.PCOptions["aa:bb:cc:dd:ee:02"].Answer != id1 || c.PCOptions["aa:bb:cc:dd:ee:03"].Answer != NoAnswer {
		t.Errorf("PC choices: %v", c.PCOptions)
	}
}

func TestAPCsChoiceWinsOverTheIsosAndNoneKeepsTheFileAway(t *testing.T) {
	c := Config{
		Answers:    map[string]AnswerFile{id1: {Name: "iso's", Kind: "preseed", Content: "a"}, id2: {Name: "pc's", Kind: "preseed", Content: "b"}},
		ISOAnswers: map[string]string{"abc1234567": id1},
		PCOptions:  map[string]PCOptions{"aa:bb:cc:dd:ee:01": {Answer: id2}, "aa:bb:cc:dd:ee:02": {Answer: NoAnswer}},
	}
	for mac, want := range map[string]string{"aa:bb:cc:dd:ee:09": id1, "aa:bb:cc:dd:ee:01": id2, "aa:bb:cc:dd:ee:02": ""} {
		id, _, ok := c.AnswerFor("abc1234567", mac)
		if id != want || ok != (want != "") {
			t.Errorf("%s: got %q %v, want %q", mac, id, ok, want)
		}
	}
	if _, _, ok := c.AnswerFor("ffff999999", "aa:bb:cc:dd:ee:09"); ok {
		t.Error("an ISO with no file and a PC with no choice gets nothing")
	}
}

func TestSecretsInAnswerFilesAreMaskedAndComeBackWhenTheMaskedTextIsSaved(t *testing.T) {
	file := strings.Join([]string{
		"d-i netcfg/get_hostname string {{hostname}}",
		"d-i passwd/user-password password hunter2",
		"d-i passwd/root-password-crypted password $6$salt$hash",
		"#_preseed_V1 password in a comment",
		"autoinstall:",
		"  identity:",
		"    hostname: lab",
		"    password: \"$6$abc$def\"",
		"rootpw --plaintext s3cret",
		"user --name=ann --password=pw1 --groups=wheel",
		"<user_password>topsecret</user_password>",
		"<hostname>keep-me</hostname>",
	}, "\n")
	masked := MaskAnswer(file)
	for _, leak := range []string{"hunter2", "$6$salt$hash", "$6$abc$def", "s3cret", "pw1", "topsecret"} {
		if strings.Contains(masked, leak) {
			t.Errorf("%q is still shown:\n%s", leak, masked)
		}
	}
	for _, keep := range []string{"{{hostname}}", "hostname: lab", "<hostname>keep-me</hostname>", "--name=ann", "password in a comment"} {
		if !strings.Contains(masked, keep) {
			t.Errorf("%q was masked:\n%s", keep, masked)
		}
	}
	if got := RestoreMasked(file, masked); got != file {
		t.Errorf("saving the masked text must give the file back:\n%s", got)
	}
	// a person edits a line next to a masked one, and adds a new secret by hand: only the masked lines come back
	edited := strings.Replace(masked, "hostname: lab", "hostname: lab2", 1) + "\nd-i extra/secret string new1"
	got := RestoreMasked(file, edited)
	if !strings.Contains(got, "hostname: lab2") || !strings.Contains(got, "hunter2") || !strings.Contains(got, "new1") {
		t.Errorf("%s", got)
	}
	// a masked line that was changed no longer matches an old one and stays masked: the caller refuses it
	if bad := RestoreMasked(file, strings.Replace(masked, "rootpw --plaintext "+Masked, "rootpw --iscrypted "+Masked, 1)); !strings.Contains(bad, "rootpw --iscrypted "+Masked) {
		t.Errorf("%s", bad)
	}
}
