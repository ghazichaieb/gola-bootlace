package httpd

import (
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
	"github.com/ghazichaieb/gola-bootlace/internal/menu"
)

func TestPreseedHostnameIsSentBeforeNetworkingAndMatchesRenderedFile(t *testing.T) {
	for _, tc := range []struct{ name, mac, hostname string }{
		{"Front Desk #3", ansMAC, "front-desk-3"},
		{"", ansMAC, "pc-ddee01"},
		{"!!!", ansMAC, "pc-ddee01"},
		{"Desk && Reset", ansMAC, "desk-reset"},
		{"", "", "pc"},
	} {
		t.Run(tc.hostname+tc.name, func(t *testing.T) {
			s, _ := answerServer(t, func(c *config.Config) {
				c.Answers[ansID] = config.AnswerFile{Name: "Debian", Kind: "preseed", Content: "d-i netcfg/get_hostname string {{hostname}}\n"}
				c.PCNames = map[string]string{tc.mac: tc.name}
			})
			cfg := s.settings()
			for _, kernel := range []string{"kernel http://s/linux vga=788 --- quiet", "kernel http://s/linux vga=788"} {
				entry := menu.Entry{Kind: "Debian installer", Commands: []string{"echo Starting", kernel, "initrd http://s/initrd", "boot"}}
				original := append([]string(nil), entry.Commands...)
				got := s.entryForPC(cfg, ansISO, entry, config.PCOptions{}, tc.mac)
				before, _, _ := strings.Cut(got.Commands[1], " ---")
				if !strings.Contains(before, " hostname="+tc.hostname+" netcfg/hostname="+tc.hostname) || strings.Count(got.Commands[1], " hostname=") != 1 {
					t.Fatalf("hostname missing before separator: %s", got.Commands[1])
				}
				if !reflect.DeepEqual(entry.Commands, original) || got.Commands[0] != original[0] || got.Commands[2] != original[2] {
					t.Fatal("shared or unrelated commands changed")
				}
			}
			rec := httptest.NewRecorder()
			s.answers(rec, httptest.NewRequest("GET", s.answerBase(ansID, tc.mac)+"preseed.cfg", nil))
			if rec.Code != 200 || rec.Body.String() != "d-i netcfg/get_hostname string "+tc.hostname+"\n" {
				t.Fatalf("rendered hostname differs: %d %q", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestExplicitPreseedHostnameOptionsArePreserved(t *testing.T) {
	s, _ := answerServer(t, func(c *config.Config) {
		c.Answers[ansID] = config.AnswerFile{Name: "Debian", Kind: "preseed", Content: "x"}
	})
	for _, arg := range []string{"hostname=custom", "netcfg/get_hostname=custom", "netcfg/hostname=custom"} {
		entry := menu.Entry{Kind: "Debian installer", Commands: []string{"kernel http://s/linux --- quiet", "boot"}}
		got := s.entryForPC(s.settings(), ansISO, entry, config.PCOptions{Args: arg}, ansMAC)
		if !strings.Contains(got.Commands[0], arg) || strings.Contains(got.Commands[0], "hostname=front-desk-3") {
			t.Fatalf("explicit PC option overridden: %s", got.Commands[0])
		}
		entry.Commands[0] = "kernel http://s/linux " + arg + " --- quiet"
		got = s.entryForPC(s.settings(), ansISO, entry, config.PCOptions{}, ansMAC)
		if !strings.Contains(got.Commands[0], arg) || strings.Contains(got.Commands[0], "hostname=front-desk-3") {
			t.Fatalf("explicit ISO option overridden: %s", got.Commands[0])
		}
	}
}
