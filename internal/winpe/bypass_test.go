package winpe

import (
	"strings"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
)

func TestSetupChecksAreSkippedByLabConfigValuesWrittenBeforeSetupStarts(t *testing.T) {
	base := Settings{Host: "10.0.0.1", Share: "isos", Folder: "abc", User: "u", Password: "p", Port: 445}
	if plain := Cmd(base); strings.Contains(plain, "LabConfig") {
		t.Fatalf("nothing is skipped unless asked for:\n%s", plain)
	}
	checks := config.SetupChecks{TPM: true, Storage: true}
	base.Bypass = checks.LabConfig()
	got := Cmd(base)
	for _, want := range []string{`reg add HKLM\SYSTEM\Setup\LabConfig /v BypassTPMCheck /t REG_DWORD /d 1 /f`, `reg add HKLM\SYSTEM\Setup\LabConfig /v BypassStorageCheck /t REG_DWORD /d 1 /f`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "BypassRAMCheck") || strings.Contains(got, "BypassSecureBootCheck") || strings.Contains(got, "BypassCPUCheck") {
		t.Errorf("a check that was not asked for is skipped:\n%s", got)
	}
	// the values are in place before Setup starts
	if strings.Index(got, "BypassTPMCheck") > strings.Index(got, `setup.exe`) {
		t.Errorf("the values must be written before Setup starts:\n%s", got)
	}
	all := config.SetupChecks{TPM: true, SecureBoot: true, RAM: true, CPU: true, Storage: true}.LabConfig()
	if strings.Join(all, ",") != "BypassTPMCheck,BypassSecureBootCheck,BypassRAMCheck,BypassCPUCheck,BypassStorageCheck" {
		t.Errorf("%v", all)
	}
}
