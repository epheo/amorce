package engine_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/epheo/amorce/internal/demo"
	"github.com/epheo/amorce/internal/engine"
	"github.com/epheo/amorce/internal/site"
)

func waitFor(t *testing.T, e *engine.Engine, want func(engine.State) bool) engine.State {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if st := e.Snapshot(); want(st) {
			return st
		}
		time.Sleep(20 * time.Millisecond)
	}
	st := e.Snapshot()
	t.Fatalf("step %d, join %+v", st.Step, st.Join)
	return st
}

func TestJoinWithWords(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e := engine.New(&demo.System{Speed: 1000})
	e.Start(ctx)
	st := waitFor(t, e, func(s engine.State) bool { return len(s.Clusters) == 1 })
	c := st.Clusters[0]

	e.Join(ctx, c, strings.Fields("one two three"))
	st = waitFor(t, e, func(s engine.State) bool { return s.Join != nil && !s.Join.Busy })
	if st.Join.Err == "" || st.Profile != "" {
		t.Fatalf("wrong words joined: %+v", st.Join)
	}
	e.CancelJoin()

	e.Join(ctx, c, strings.Fields("one two three four five six"))
	st = waitFor(t, e, func(s engine.State) bool { return s.Step == engine.StepDisks })
	if st.Profile != site.Worker || st.PurposeID != "storage" || st.Hostname != "srv-03" || st.ReportTo.Token != "demo" {
		t.Fatalf("joined as %q %q %q %+v", st.Profile, st.PurposeID, st.Hostname, st.ReportTo)
	}

	// Choosing a purpose makes a new cluster: the joined site is forgotten.
	e.ChoosePurpose("kubernetes")
	st = e.Snapshot()
	if st.Profile != "" || st.ReportTo != nil || st.SiteFile != nil || st.Join != nil {
		t.Fatalf("still joined: %q %+v", st.Profile, st.ReportTo)
	}
}

func TestNextName(t *testing.T) {
	for _, c := range []struct {
		first string
		taken []string
		want  string
	}{
		{"store-01", nil, "store-02"},
		{"store-01", []string{"store-02", "store-03"}, "store-04"},
		{"node9", nil, "node10"},
		{"nas", nil, "nas-02"},
	} {
		if got := engine.NextName(c.first, c.taken); got != c.want {
			t.Errorf("%s %v: %s, want %s", c.first, c.taken, got, c.want)
		}
	}
}
