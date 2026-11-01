package config

import "testing"

const savedWay = "0123456789abcdef0123456789abcdef"

func TestBootEntrySettingsSurviveRestartAndAreIsolated(t *testing.T) {
	s, path := open(t)
	settings := ISOEntrySettings{Default: savedWay, Entries: map[string]EntryOptions{savedWay: {Title: " Safe graphics ", Args: " nomodeset "}}}
	if err := s.Update(func(c *Config) error { c.ISOEntries = map[string]ISOEntrySettings{"0123456789": settings}; return nil }); err != nil {
		t.Fatal(err)
	}
	snapshot := s.Get()
	v := snapshot.ISOEntries["0123456789"]
	v.Entries[savedWay] = EntryOptions{Hidden: true}
	reopened, _, err := Open(path, Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, store := range []*Store{s, reopened} {
		got := store.Get().ISOEntries["0123456789"]
		if got.Default != savedWay || got.Entries[savedWay].Title != "Safe graphics" || got.Entries[savedWay].Args != "nomodeset" || got.Entries[savedWay].Hidden {
			t.Fatalf("settings changed: %+v", got)
		}
	}
	if err := s.Update(func(c *Config) error {
		v := c.ISOEntries["0123456789"]
		v.Entries[savedWay] = EntryOptions{Hidden: true}
		return nil
	}); err == nil {
		t.Fatal("hidden default accepted")
	}
	if s.Get().ISOEntries["0123456789"].Entries[savedWay].Hidden {
		t.Fatal("failed update changed live settings")
	}
}

func TestBootEntrySettingsRejectUnsafeInput(t *testing.T) {
	for _, options := range []EntryOptions{{Title: "bad\nentry"}, {Args: "quiet\nboot"}, {Args: "quiet && shell"}} {
		c := Config{ISOEntries: map[string]ISOEntrySettings{"0123456789": {Entries: map[string]EntryOptions{savedWay: options}}}}
		if c.Validate() == nil {
			t.Fatalf("accepted %+v", options)
		}
	}
	for _, key := range []string{"", "../../entry", "abcd"} {
		c := Config{ISOEntries: map[string]ISOEntrySettings{"0123456789": {Default: key, Entries: map[string]EntryOptions{key: {}}}}}
		if c.Validate() == nil {
			t.Fatalf("accepted entry id %q", key)
		}
	}
}

func TestISOActionAcceptsTheUsualStartAndOneEntryOnly(t *testing.T) {
	entry := "0123456789abcdef0123456789abcdef"
	for _, a := range []string{"iso:0123456789", "iso:0123456789@" + entry} {
		if !ValidAction(a) {
			t.Errorf("%q should be valid", a)
		}
	}
	if id, e, ok := ISOAction("iso:0123456789@" + entry); !ok || id != "0123456789" || e != entry {
		t.Errorf("split: %q %q %v", id, e, ok)
	}
	for _, a := range []string{"iso:0123456789@", "iso:0123456789@" + entry + "0", "iso:0123456789@XYZ", "iso:0123456789@" + entry + "@" + entry, "native:0123456789@" + entry, "iso:@" + entry, "iso:short@" + entry} {
		if ValidAction(a) {
			t.Errorf("%q should be refused", a)
		}
	}
}
