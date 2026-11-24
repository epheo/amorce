package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/epheo/microshift-installer/internal/report"
	"github.com/epheo/microshift-installer/internal/site"
)

// reportLoop tells the first machine how this install goes. It sends on
// every change worth a line on the board, and again every minute; a send
// that fails is retried at the next change or tick, never waited on.
func (e *Engine) reportLoop(ctx context.Context, to site.Report) {
	ch, stop := e.Subscribe()
	defer stop()
	addr := to.HostPort(report.Port)
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	var sent report.Report
	var sentAt time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-ch:
		case <-tick.C:
		}
		r := reportOf(e.Snapshot())
		same := r.State == sent.State && r.Step == sent.Step && r.Detail == sent.Detail &&
			r.Name == sent.Name && r.Address == sent.Address && r.Progress-sent.Progress < 0.05
		if same && time.Since(sentAt) < time.Minute {
			continue
		}
		if report.Send(ctx, addr, to.Token, r) == nil {
			sent, sentAt = r, time.Now()
		}
	}
}

var stepNames = map[Step]string{
	StepHello: "Welcome", StepNetwork: "Network", StepPurpose: "Purpose",
	StepDisks: "Disks", StepName: "Name", StepConfirm: "Confirm",
}

// reportOf is the board's line for this machine.
func reportOf(s State) report.Report {
	r := report.Report{Name: s.Hostname, Serial: s.Machine.Serial, Expected: s.SiteNames}
	if s.Site != nil && s.Site.Name != "" {
		r.Name = s.Site.Name
	}
	for _, n := range s.Machine.NICs {
		if r.MAC == "" || n.Name == s.Net.Config.Iface {
			r.MAC = n.MAC
		}
	}
	if r.Name == "" {
		r.Name = "serial " + s.Machine.Serial
		if s.Machine.Serial == "" {
			r.Name = "MAC " + r.MAC
		}
	}
	if s.Net.Up && s.Net.Config.Address.IsValid() {
		r.Address = s.Net.Config.Address.Addr().String()
	}
	switch {
	case s.Install.Err != "":
		r.State, r.Detail = report.Failed, s.Install.Err
	case s.Step == StepReady:
		r.State = report.Installed
	case s.Step == StepInstalling:
		r.State, r.Progress = report.Installing, s.Install.Progress
		for _, st := range s.Install.Steps {
			if st.Status == Running {
				r.Step = st.Title
			}
		}
	case s.Site != nil && s.Site.Problem != "":
		r.State, r.Detail = report.Waiting, s.Site.Problem
	case s.Site != nil && s.Site.Checking:
		r.State, r.Detail = report.Waiting, "Checking the site file's answers."
	case s.Site != nil && s.Site.Countdown > 0:
		r.State, r.Detail = report.Waiting, fmt.Sprintf("Installing in %d s.", s.Site.Countdown)
	default:
		r.State, r.Detail = report.Waiting, "A person is on the "+stepNames[s.Step]+" screen."
	}
	return r
}
