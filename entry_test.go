package mlog

import "testing"

func TestEntryPool(t *testing.T) {
	e := getEntry()
	if e == nil {
		t.Fatal("getEntry returned nil")
	}
	if cap(e.Fields) < 16 {
		t.Fatalf("Fields capacity %d, want >= 16", cap(e.Fields))
	}

	e.Message = "test"
	e.Fields = append(e.Fields, Int("k", 1))
	putEntry(e)

	e2 := getEntry()
	if e2.Message != "" {
		t.Fatal("entry not reset after put")
	}
	if len(e2.Fields) != 0 {
		t.Fatal("Fields not cleared after put")
	}
	putEntry(e2)
}
