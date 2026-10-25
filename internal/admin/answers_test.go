package admin

import (
	"encoding/xml"
	"strings"
	"testing"
)

const answerText = "d-i netcfg/get_hostname string {{hostname}}\nd-i passwd/user-password password hunter2\n"

func createAnswer(t *testing.T, r *rig, name, kind, content string) string {
	t.Helper()
	code, out := r.do("POST", "/api/answers", map[string]any{"name": name, "kind": kind, "content": content}, nil)
	if code != 200 {
		t.Fatalf("create: %d %v", code, out)
	}
	return out["id"].(string)
}

func TestAnswerFilesAreCreatedListedAndNeverListedWithTheirText(t *testing.T) {
	r := newRig(t)
	r.login()
	id := createAnswer(t, r, "Lab", "preseed", answerText)
	code, out := r.do("GET", "/api/answers", nil, nil)
	list := out["answers"].([]any)
	if code != 200 || len(list) != 1 {
		t.Fatalf("%d %v", code, out)
	}
	first := list[0].(map[string]any)
	if first["id"] != id || first["name"] != "Lab" || first["kind"] != "preseed" || first["size"].(float64) != float64(len(answerText)) {
		t.Fatalf("%v", first)
	}
	if _, has := first["content"]; has || strings.Contains(strings.Join(strings.Fields(strings.ReplaceAll(walkStrings(out), "\n", " ")), " "), "hunter2") {
		t.Fatalf("the list holds the text: %v", out)
	}
	for _, bad := range []map[string]any{{"name": "", "kind": "preseed", "content": "x"}, {"name": "x", "kind": "windows", "content": "x"}, {"name": "x", "kind": "preseed", "content": "a\x00b"}} {
		if code, _ := r.do("POST", "/api/answers", bad, nil); code != 400 {
			t.Errorf("%v was accepted", bad)
		}
	}
}

func walkStrings(m map[string]any) string {
	var b strings.Builder
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, e := range x {
				b.WriteString(k + " ")
				walk(e)
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		case string:
			b.WriteString(x + " ")
		}
	}
	walk(m)
	return b.String()
}

func TestAFileIsShownMaskedUnlessAskedForAndSavingTheMaskedTextKeepsTheSecrets(t *testing.T) {
	r := newRig(t)
	r.login()
	id := createAnswer(t, r, "Lab", "preseed", answerText)
	_, shown := r.do("GET", "/api/answers/"+id, nil, nil)
	text := shown["content"].(string)
	if strings.Contains(text, "hunter2") || !strings.Contains(text, "password ********") || shown["masked"] != true || !strings.Contains(text, "{{hostname}}") {
		t.Fatalf("%q", text)
	}
	// the person changes the host name line and saves the masked text back
	edited := strings.Replace(text, "{{hostname}}", "lab-{{mac-dash}}", 1)
	if code, out := r.do("PUT", "/api/answers/"+id, map[string]any{"content": edited}, nil); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	_, full := r.do("GET", "/api/answers/"+id+"?reveal=1", nil, nil)
	if got := full["content"].(string); got != "d-i netcfg/get_hostname string lab-{{mac-dash}}\nd-i passwd/user-password password hunter2\n" || full["masked"] != false {
		t.Fatalf("%q", got)
	}
	// a line that shows the mask but no longer matches an old line would store the mask as the password: refused
	broken := strings.Replace(text, "d-i passwd/user-password password", "d-i passwd/user-password-again password", 1)
	if code, out := r.do("PUT", "/api/answers/"+id, map[string]any{"content": broken}, nil); code != 400 || !strings.Contains(out["error"].(string), "********") {
		t.Fatalf("%d %v", code, out)
	}
	if _, again := r.do("GET", "/api/answers/"+id+"?reveal=1", nil, nil); !strings.Contains(again["content"].(string), "hunter2") {
		t.Fatal("a refused save must change nothing")
	}
}

func TestAttachingAFileToAnIsoOrAPCAndDeletingIt(t *testing.T) {
	r := newRig(t)
	r.login()
	id := createAnswer(t, r, "Lab", "preseed", answerText)
	if code, _ := r.do("PUT", "/api/isos/0123456789", map[string]any{"answer": "ffffffffffff"}, nil); code != 400 {
		t.Fatal("a file that does not exist was attached")
	}
	if code, out := r.do("PUT", "/api/isos/0123456789", map[string]any{"answer": id}, nil); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	if code, _ := r.do("PUT", "/api/pcs/aa:bb:cc:dd:ee:01/answer", map[string]any{"answer": "ffffffffffff"}, nil); code != 400 {
		t.Fatal("a file that does not exist was attached to a PC")
	}
	if code, out := r.do("PUT", "/api/pcs/aa:bb:cc:dd:ee:01/answer", map[string]any{"answer": id}, nil); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	// the options dialog sends arguments only: the PC's file choice stays
	if code, _ := r.do("PUT", "/api/pcs/aa:bb:cc:dd:ee:01/options", map[string]any{"args": "nomodeset"}, nil); code != 200 {
		t.Fatal("options")
	}
	c := r.store.Get()
	if c.ISOAnswers["0123456789"] != id || c.PCOptions["aa:bb:cc:dd:ee:01"].Answer != id || c.PCOptions["aa:bb:cc:dd:ee:01"].Args != "nomodeset" {
		t.Fatalf("%+v %+v", c.ISOAnswers, c.PCOptions)
	}
	_, out := r.do("GET", "/api/answers", nil, nil)
	first := out["answers"].([]any)[0].(map[string]any)
	if len(first["isos"].([]any)) != 1 || len(first["pcs"].([]any)) != 1 {
		t.Fatalf("what the file is attached to is listed: %v", first)
	}
	if code, _ := r.do("DELETE", "/api/answers/"+id, nil, nil); code != 200 {
		t.Fatal("delete")
	}
	c = r.store.Get()
	if len(c.Answers) != 0 || len(c.ISOAnswers) != 0 || c.PCOptions["aa:bb:cc:dd:ee:01"].Answer != "" || c.PCOptions["aa:bb:cc:dd:ee:01"].Args != "nomodeset" {
		t.Fatalf("deleting a file takes away what named it, and nothing else: %+v", c)
	}
	if code, _ := r.do("DELETE", "/api/answers/"+id, nil, nil); code != 404 {
		t.Fatal("deleting twice")
	}
	if code, _ := r.do("PUT", "/api/isos/0123456789", map[string]any{"answer": ""}, nil); code != 200 {
		t.Fatal("taking it away")
	}
}

func TestTheTemplatesAreThereForEveryKindAndTheLibraryNeedsASignIn(t *testing.T) {
	r := newRig(t)
	if code, _ := r.do("GET", "/api/answers", nil, nil); code != 401 {
		t.Fatalf("the list is for signed-in people: %d", code)
	}
	if code, _ := r.do("GET", "/api/answers/templates", nil, nil); code != 401 {
		t.Fatalf("the templates too: %d", code)
	}
	r.login()
	_, out := r.do("GET", "/api/answers/templates", nil, nil)
	templates := out["templates"].(map[string]any)
	for _, kind := range []string{"preseed", "autoinstall", "kickstart", "autoyast"} {
		text, _ := templates[kind].(string)
		if !strings.Contains(text, "ERASES") || !strings.Contains(text, "{{hostname}}") {
			t.Errorf("%s: a template says that it erases disks and uses the PC's name", kind)
		}
		if code, o := r.do("POST", "/api/answers", map[string]any{"name": kind, "kind": kind, "content": text}, nil); code != 200 {
			t.Errorf("%s: the template is not accepted: %d %v", kind, code, o)
		}
	}
}

// The Debian installer refuses the names that its user-setup reserves ("The username you entered (admin) is reserved for use by the system"), found by running the
// template (#59): the template must not use one.
func TestThePreseedTemplateDoesNotUseAReservedUserName(t *testing.T) {
	reserved := map[string]bool{"root": true, "admin": true, "daemon": true, "bin": true, "sys": true, "sync": true, "games": true, "man": true, "lp": true, "mail": true,
		"news": true, "uucp": true, "proxy": true, "www-data": true, "backup": true, "list": true, "irc": true, "nobody": true, "operator": true}
	for _, line := range strings.Split(answerTemplates["preseed"], "\n") {
		if rest, ok := strings.CutPrefix(line, "d-i passwd/username string "); ok {
			if name := strings.TrimSpace(rest); reserved[name] {
				t.Errorf("the preseed template gives the first user the reserved name %q", name)
			}
			return
		}
	}
	t.Error("the preseed template has no user name line")
}

// The address of an answer file works without a login (issue #60): a plain password in one is said out loud when the file is saved, listed and attached. Nothing is refused.
func TestAPlainPasswordInAnAnswerFileIsWarnedAboutWhereverItIsSavedListedOrAttached(t *testing.T) {
	r := newRig(t)
	r.login()
	code, out := r.do("POST", "/api/answers", map[string]any{"name": "Lab", "kind": "preseed", "content": answerText}, nil)
	warn, _ := out["warnings"].([]any)
	if code != 200 || len(warn) != 1 || !strings.Contains(warn[0].(string), "line 2") || !strings.Contains(warn[0].(string), "passwd/user-password") {
		t.Fatalf("create: %d %v", code, out)
	}
	id := out["id"].(string)
	if strings.Contains(walkStrings(out), "hunter2") {
		t.Fatal("the warning repeats the password")
	}
	_, list := r.do("GET", "/api/answers", nil, nil)
	if plain := list["answers"].([]any)[0].(map[string]any)["plain"].([]any); len(plain) != 1 {
		t.Fatalf("the list says nothing: %v", list)
	}
	if _, got := r.do("GET", "/api/answers/"+id, nil, nil); len(got["warnings"].([]any)) != 1 {
		t.Fatalf("get: %v", got)
	}
	if _, att := r.do("PUT", "/api/isos/0123456789", map[string]any{"answer": id}, nil); len(att["warnings"].([]any)) != 1 {
		t.Fatalf("attaching to an ISO: %v", att)
	}
	if _, att := r.do("PUT", "/api/pcs/aa:bb:cc:dd:ee:01/answer", map[string]any{"answer": id}, nil); len(att["warnings"].([]any)) != 1 {
		t.Fatalf("attaching to a PC: %v", att)
	}
	// a hash is not warned about, and the warning goes when the password is replaced
	fixed := "d-i netcfg/get_hostname string {{hostname}}\nd-i passwd/user-password-crypted password $6$salt$hash\n"
	if code, out := r.do("PUT", "/api/answers/"+id, map[string]any{"content": fixed}, nil); code != 200 || len(out["warnings"].([]any)) != 0 {
		t.Fatalf("update: %d %v", code, out)
	}
	if _, att := r.do("PUT", "/api/isos/0123456789", map[string]any{"answer": id}, nil); len(att["warnings"].([]any)) != 0 {
		t.Fatalf("attaching a clean file: %v", att)
	}
}

func TestTheShippedTemplatesAreNotWarnedAbout(t *testing.T) {
	r := newRig(t)
	r.login()
	for kind, text := range answerTemplates {
		code, out := r.do("POST", "/api/answers", map[string]any{"name": kind, "kind": kind, "content": text}, nil)
		if code != 200 || len(out["warnings"].([]any)) != 0 {
			t.Errorf("%s: %d %v", kind, code, out)
		}
	}
}

func TestTheSwitchForTheSameAddressIsSavedAndNeedsASignIn(t *testing.T) {
	r := newRig(t)
	if code, _ := r.do("PUT", "/api/answers/settings", map[string]any{"sameAddress": true}, nil); code != 401 {
		t.Fatalf("%d", code)
	}
	r.login()
	if code, _ := r.do("PUT", "/api/answers/settings", map[string]any{"sameAddress": true}, nil); code != 200 || !r.store.Get().AnswersSameAddress {
		t.Fatal("not saved")
	}
	if _, out := r.do("GET", "/api/answers", nil, nil); out["sameAddress"] != true {
		t.Fatalf("%v", out)
	}
}

func TestTheXmlTemplatesAreWellFormed(t *testing.T) {
	for _, kind := range []string{"autoyast", "unattend"} {
		d := xml.NewDecoder(strings.NewReader(answerTemplates[kind]))
		for {
			if _, err := d.Token(); err != nil {
				if err.Error() != "EOF" {
					t.Errorf("%s template is not well formed XML: %v", kind, err)
				}
				break
			}
		}
	}
}
