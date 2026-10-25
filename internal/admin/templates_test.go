package admin

import (
	"strings"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
)

// The templates are what a person starts from: they take a password HASH (a file is readable by anyone on the network, so a plain password must not be what they invite), and the
// line that holds it is masked on the page.
func TestTheTemplatesAskForPasswordHashesAndTheirPasswordLinesAreMasked(t *testing.T) {
	for kind, text := range answerTemplates {
		if _, ok := config.AnswerKindOf(kind); !ok {
			t.Errorf("%s is not a kind", kind)
		}
		if strings.Contains(text, "CHANGE-ME-NOW") || strings.Contains(text, "--plaintext") || strings.Contains(strings.ToLower(text), "password-again") {
			t.Errorf("%s: the template still invites a plain password", kind)
		}
		if !strings.Contains(strings.ToLower(text), "hash") && !strings.Contains(strings.ToLower(text), "crypted") {
			t.Errorf("%s: the template does not say that the password is a hash", kind)
		}
		masked := config.MaskAnswer(text)
		if !strings.Contains(masked, config.Masked) {
			t.Errorf("%s: no line of the template is masked", kind)
		}
		for _, placeholder := range []string{"PUT-A-PASSWORD-HASH-HERE", "PUT-A-CRYPTED-PASSWORD-HERE"} {
			if strings.Contains(masked, placeholder) {
				t.Errorf("%s: the password line shows %s unmasked", kind, placeholder)
			}
		}
	}
}
