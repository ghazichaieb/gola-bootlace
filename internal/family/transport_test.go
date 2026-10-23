package family

import "testing"

func TestTheBestWayTheServerCanDoIsPicked(t *testing.T) {
	both := withISCSI(withNBD(urls))
	for name, tc := range map[string]struct {
		u    URLs
		from []Transport
		want Transport
	}{
		"network disk first":                   {both, []Transport{NBD, ISCSI, Memory}, NBD},
		"iSCSI when there is no network disk":  {withISCSI(urls), []Transport{NBD, ISCSI, Memory}, ISCSI},
		"the whole ISO when there is neither":  {urls, []Transport{NBD, ISCSI, Memory}, Memory},
		"what the system cannot do is passed":  {both, []Transport{ISCSI, Memory}, ISCSI},
		"the last one when nothing is offered": {urls, []Transport{NBD, ISCSI}, ISCSI},
		"HTTP needs only the web server":       {urls, []Transport{NBD, HTTP}, HTTP},
	} {
		if got := tc.u.pick(tc.from...); got != tc.want {
			t.Errorf("%s: %d, want %d", name, got, tc.want)
		}
	}
}

func TestTheNetworkDiskNeedsTheServerTheArchiveAndTheAddress(t *testing.T) {
	u := withNBD(urls)
	if !u.has(NBD) {
		t.Fatal("a complete network disk setting is not usable")
	}
	for name, cut := range map[string]func(*URLs){
		"no server":  func(u *URLs) { u.NBD = "" },
		"no archive": func(u *URLs) { u.Extra = "" },
		"no address": func(u *URLs) { u.NBDHost = "" },
	} {
		x := u
		cut(&x)
		if x.has(NBD) {
			t.Errorf("%s: the network disk is taken as usable", name)
		}
	}
}
