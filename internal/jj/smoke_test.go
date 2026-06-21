package jj

import "testing"

// TestSmoke exercises the runner against the current jj repo (gojo is itself
// a jj repo). It validates config loading, log/status parsing, and diffing.
func TestSmoke(t *testing.T) {
	cfg, err := LoadConfig()
	if err != nil {
		t.Skipf("not in a jj repo: %v", err)
	}
	r := NewRunner(cfg)

	entries, err := r.Log(20)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("Log returned no entries")
	}
	t.Logf("log entries: %d", len(entries))
	e := entries[0]
	if e.ChangeID == "" || e.CommitID == "" {
		t.Fatalf("empty ids in first entry: %+v", e)
	}
	t.Logf("first: change=%s commit=%s wc=%v subject=%q bookmarks=%v",
		e.ChangeID, e.CommitID, e.IsWorkingCopy, e.Subject, e.Bookmarks)

	if _, err := r.Status(); err != nil {
		t.Fatalf("Status: %v", err)
	}

	// Diff the most recent non-working-copy commit that has changes.
	diff, err := r.Diff(entries[0].CommitID)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	t.Logf("diff bytes: %d", len(diff))
}
