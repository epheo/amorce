package live

import (
	"context"
	"errors"
	"net/http"

	"github.com/epheo/microshift-installer/internal/engine"
	"github.com/epheo/microshift-installer/internal/link"
	"github.com/epheo/microshift-installer/internal/report"
)

func (s *System) Clusters(ctx context.Context, found func(engine.Cluster)) {
	err := link.Listen(ctx, func(b link.Beacon) {
		found(engine.Cluster{Site: b.Site, Name: b.Name, Address: b.Address, Purpose: b.Purpose, Open: b.Open})
	})
	if err != nil && ctx.Err() == nil {
		s.logf("listening for first machines: %v", err)
	}
}

func (s *System) Admit(ctx context.Context, address string, words []string, me report.Report) (engine.Admission, error) {
	a, err := report.Admit(ctx, address, link.Secret(words), me)
	var le *link.Error
	switch {
	case errors.As(err, &le) && le.Status == http.StatusUnauthorized:
		return engine.Admission{}, errors.New("Those are not the words on its screen.")
	case err != nil:
		return engine.Admission{}, err
	}
	return engine.Admission{Site: []byte(a.Site), Purpose: a.Purpose, Names: a.Names}, nil
}
