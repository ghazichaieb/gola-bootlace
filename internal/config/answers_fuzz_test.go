package config

import "testing"

// Saving an answer file that was shown masked must give back exactly the file that was there: if a mask were lossy, one click on Save would destroy a password.
func FuzzMaskedFileRoundTrip(f *testing.F) {
	f.Add("d-i passwd/user-password password S3cret\nd-i passwd/user-password-again password S3cret\n")
	f.Add("autoinstall:\n  identity:\n    password: \"$6$abc$def\"\n    hostname: x\n")
	f.Add("rootpw --iscrypted $6$x\nuser --name=a --password=pw\nsshkey --username=a \"ssh-ed25519 AAAA\"\n")
	f.Add("<user_password>pw</user_password>\r\n<encrypted config:type=\"boolean\">false</encrypted>\r\n")
	f.Add("key: ********\nkey: real\n\n\n# comment with password\nsecret=1\nsecret=2\n")
	f.Fuzz(func(t *testing.T, text string) {
		masked := MaskAnswer(text)
		if got := RestoreMasked(text, masked); got != text {
			t.Fatalf("saving the masked file changed it:\n%q\nbecame\n%q\n(masked as %q)", text, got, masked)
		}
	})
}
