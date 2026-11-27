package ui

import (
	"time"

	"github.com/epheo/amorce/internal/report"
)

// DemoHome is the mockups' first machine, for the demo and the tests.
func DemoHome() HomeInfo {
	return HomeInfo{
		Name: "store-01", Address: []string{"10.20.120.11"}, Health: "degraded", Failed: 1,
		Serial: "CZ2041XY7J", Image: "ghcr.io/epheo/stornas-os:latest", Site: "lab",
		Cluster: "first machine, 1 worker joined", CanAdd: true,
		Words:      []string{"aardvark", "gigabyte", "outhouse", "reunion", "uncle", "zigzagged"},
		WordsUntil: time.Date(2026, 11, 24, 15, 42, 0, 0, time.Local),
		Notes: []Note{{From: "stornas", Lines: []string{
			"stornas web UI: https://10.20.120.11:30080",
			"TLS certificate SHA-256: 3A:4F:9C:21:0B:7E:D2:88:15:C4:6A:F0:93:2E:B7:5D:11:A8:CE:60:4B:9F:72:E3:08:D5:36:AC:1F:84:E9:27",
			"Initial password for admin: correct-horse-staple (the UI asks to change it)",
		}}},
		Board: []report.Report{
			{Name: "store-01", State: report.Running, Address: "10.20.120.11"},
			{Name: "store-06", State: report.Running, Address: "10.20.120.16", Role: report.Worker},
			{Name: "store-07", State: report.Joining, Role: report.Worker, Detail: "starting MicroShift as a worker"},
			{Name: "store-02", State: report.Installing, Step: "Copy the system image", Progress: 0.43},
			{Name: "store-03", State: report.Waiting, Detail: "Stopped: the site file erases blank disks only."},
			{Name: "store-04", State: report.Failed, Detail: "bootc: no space left on device"},
			{Name: "store-05"},
		},
	}
}
