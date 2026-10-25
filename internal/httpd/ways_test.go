package httpd

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
	"github.com/ghazichaieb/gola-bootlace/internal/family"
	"github.com/ghazichaieb/gola-bootlace/internal/isos"
	"github.com/ghazichaieb/gola-bootlace/internal/menu"
)

func waysISO(id, title string, ways ...family.Way) entryInfo {
	return entryInfo{
		ISO: isos.ISO{ID: id, Name: title + ".iso", Title: title}, Title: title, Display: title + "  [Ubuntu-style, 3.0 GB]", Boot: family.Boot{Family: "Ubuntu-style (casper)"},
		Entry: menu.Entry{Name: title, Kind: "Ubuntu-style (casper)", Commands: []string{"kernel http://s/vmlinuz boot=casper quiet", "initrd http://s/initrd", "boot"}, Guard: nil},
		Ways:  ways,
	}
}

var safeWay = family.Way{Title: "Safe graphics", Family: "Ubuntu-style (casper), by its boot menu", Special: true, Commands: []string{"kernel http://s/vmlinuz boot=casper nomodeset quiet", "initrd http://s/initrd", "boot"}}

func waysPage(s *Server, cfg config.Config, visible []entryInfo, pc config.PCOptions, platform, which string, page int, focus ...string) string {
	rec := httptest.NewRecorder()
	s.waysMenu(rec, cfg, visible, pc, "", platform, which, page, strings.Join(focus, ""))
	return strings.ReplaceAll(rec.Body.String(), "\xff", " ") // (a BIOS table lines things up with the byte 0xFF)
}

func TestTheListOfWaysHasOnlyTheIsosThatHaveSome(t *testing.T) {
	s := &Server{Base: "http://10.0.0.1:8080"}
	visible := []entryInfo{waysISO("a1", "Plain ISO"), waysISO("b2", "Ubuntu", safeWay)}
	page := waysPage(s, config.Config{}, visible, config.PCOptions{}, "efi", "list", 1)
	if strings.Contains(page, "Plain ISO") || !strings.Contains(page, "Ubuntu") || !strings.Contains(page, "ways=b2") {
		t.Fatalf("only an ISO with ways, and a line that opens its ways:\n%s", page)
	}
	if !strings.Contains(page, "menu Other ways to start an ISO\n") || !strings.Contains(page, "<< Back to the list") || !strings.Contains(page, ":back\nchain http://10.0.0.1:8080/menu?") {
		t.Fatalf("a heading and the way back:\n%s", page)
	}
}

func TestAnIsosWaysStartWithTheUsualOneAndKeepItsArgumentsPasswordAndThePCsOptions(t *testing.T) {
	s := &Server{Base: "http://10.0.0.1:8080"}
	ubuntu := waysISO("b2", "Ubuntu", safeWay)
	ubuntu.Entry.Guard = []string{"login --password x || goto denied"}
	cfg := config.Config{Args: map[string]string{"b2": "isoarg"}}
	page := waysPage(s, cfg, []entryInfo{ubuntu}, config.PCOptions{Args: "pcarg"}, "efi", "b2", 1)
	if usual, safe := strings.Index(page, "The usual start"), strings.Index(page, "Safe graphics"); usual < 0 || safe < usual {
		t.Fatalf("the usual start comes first:\n%s", page)
	}
	if !strings.Contains(page, "kernel http://s/vmlinuz boot=casper nomodeset quiet isoarg pcarg") {
		t.Errorf("a way gets the ISO's extra arguments and then the PC's:\n%s", page)
	}
	if !strings.Contains(page, "kernel http://s/vmlinuz boot=casper quiet isoarg pcarg") && !strings.Contains(page, "kernel http://s/vmlinuz boot=casper quiet pcarg") {
		t.Errorf("the usual start gets the PC's arguments:\n%s", page)
	}
	if strings.Count(page, "login --password x || goto denied") < 2 {
		t.Errorf("an ISO with a password asks for it before each way:\n%s", page)
	}
	if !strings.Contains(page, "menu How to start Ubuntu\n") {
		t.Errorf("heading:\n%s", page)
	}
	if strings.Contains(ubuntu.Entry.Commands[0], "pcarg") || strings.Contains(safeWay.Commands[0], "pcarg") {
		t.Fatal("the shared entries must not change: the next PC would get this PC's options")
	}
}

func TestAnUnknownIsoInTheWaysMenuGoesBackToTheList(t *testing.T) {
	s := &Server{Base: "http://10.0.0.1:8080"}
	page := waysPage(s, config.Config{}, []entryInfo{waysISO("b2", "Ubuntu", safeWay)}, config.PCOptions{}, "efi", "zz", 1)
	if !strings.HasPrefix(page, "#!ipxe\nchain http://10.0.0.1:8080/menu?") || strings.Contains(page, "menu How") {
		t.Fatalf("%s", page)
	}
}

func TestALongListOfWaysIsPagedAndTheBackLineIsOnEveryPage(t *testing.T) {
	s := &Server{Base: "http://10.0.0.1:8080"}
	var ways []family.Way
	for i := 0; i < 12; i++ {
		ways = append(ways, family.Way{Title: "Way " + string(rune('A'+i)), Commands: []string{"kernel http://s/v a" + string(rune('a'+i)), "boot"}})
	}
	visible := []entryInfo{waysISO("b2", "Ubuntu", ways...)}
	first := waysPage(s, config.Config{}, visible, config.PCOptions{}, "pcbios", "b2", 1)
	second := waysPage(s, config.Config{}, visible, config.PCOptions{}, "pcbios", "b2", 2, "next") // (reached with Next page)
	if !strings.Contains(first, "page 1 of 2") || !strings.Contains(first, "Next page >>") || strings.Contains(first, "Previous page") {
		t.Fatalf("first page:\n%s", first)
	}
	if !strings.Contains(second, "page 2 of 2") || !strings.Contains(second, "Previous page") || !strings.Contains(second, "set menu-default prev") {
		t.Fatalf("second page:\n%s", second)
	}
	for _, p := range []string{first, second} {
		if !strings.Contains(p, "Back to the list") {
			t.Fatalf("no way back:\n%s", p)
		}
	}
}

func TestTheMainMenuHasALineForTheWaysOnlyWhenAnIsoHasSome(t *testing.T) {
	with := menu.ScriptWith("Gola Bootlace", []menu.Entry{{Name: "Ubuntu", Commands: []string{"echo x"}}}, menu.Options{Ways: "http://s/menu?ways=list"})
	without := menu.ScriptWith("Gola Bootlace", []menu.Entry{{Name: "Ubuntu", Commands: []string{"echo x"}}}, menu.Options{})
	if !strings.Contains(with, "Other ways to start an ISO >>") || !strings.Contains(with, ":ways\nchain http://s/menu?ways=list || goto shell") {
		t.Fatalf("%s", with)
	}
	if strings.Contains(without, "Other ways") || strings.Contains(without, ":ways") {
		t.Fatalf("%s", without)
	}
}

func TestThePagesOfTheWaysKeepTheDirectionOfTheButtonThatWasPressed(t *testing.T) {
	s := &Server{Base: "http://10.0.0.1:8080"}
	var ways []family.Way
	for i := 0; i < 25; i++ {
		ways = append(ways, family.Way{Title: fmt.Sprintf("Way %02d", i), Commands: []string{fmt.Sprintf("kernel http://s/v a%02d", i), "boot"}})
	}
	visible := []entryInfo{waysISO("b2", "Ubuntu", ways...)}
	for _, tc := range []struct {
		page  int
		focus string
		want  string
	}{
		{2, "next", "next"}, {3, "next", "next"}, {4, "next", "prev"}, // forwards: Next page until there is none
		{3, "prev", "prev"}, {2, "prev", "prev"}, {1, "prev", "next"}, // backwards: Previous page until there is none
	} {
		got := waysPage(s, config.Config{}, visible, config.PCOptions{}, "pcbios", "b2", tc.page, tc.focus)
		if !strings.Contains(got, "set menu-default "+tc.want+"\n") {
			t.Errorf("page %d reached with %s: want the highlight on %s:\n%.400s", tc.page, tc.focus, tc.want, got)
		}
	}
	middle := waysPage(s, config.Config{}, visible, config.PCOptions{}, "pcbios", "b2", 2)
	if !strings.Contains(middle, "&focus=prev") || !strings.Contains(middle, "&focus=next") {
		t.Errorf("the page buttons must carry their direction:\n%s", middle)
	}
}
