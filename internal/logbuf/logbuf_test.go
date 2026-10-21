package logbuf

import "testing"

func TestLinesAreNumberedAndOnlyTheLastOnesAreKept(t *testing.T) {
	b := New(100)
	for i := 1; i <= 130; i++ {
		b.Logf("line %d\n", i)
	}
	all, last := b.After(0, 0)
	if last != 130 || len(all) != 100 || all[0].N != 31 || all[0].Text != "line 31" {
		t.Fatalf("last %d, kept %d, first %+v", last, len(all), all[0])
	}
	news, _ := b.After(125, 0)
	if len(news) != 5 || news[0].Text != "line 126" {
		t.Fatalf("after 125: %+v", news)
	}
	few, _ := b.After(0, 3)
	if len(few) != 3 || few[2].N != 130 {
		t.Fatalf("limit: %+v", few)
	}
	if none, last := b.After(130, 0); len(none) != 0 || last != 130 {
		t.Fatalf("nothing new: %+v %d", none, last)
	}
}
