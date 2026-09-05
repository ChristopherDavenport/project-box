package api

import (
	"net/http"

	"github.com/ChristopherDavenport/project-box/internal/runlog"
)

// runFrames is the run log for one run — a work item's id, or "plan-<ingress>"
// for a judgement — oldest first. What the live view showed while the run was
// on, readable after it: the tool calls and what each was doing, what the
// agent said, what policy refused. Telemetry, not the chain; a run from before
// the log existed, or one pruned by age, answers with an empty list rather
// than a 404, because "nothing kept" is an answer and "no such run" is a
// different one the chain gives.
func (s *Server) runFrames(w http.ResponseWriter, r *http.Request) {
	out, err := s.Store.Runs().Frames(r.Context(), s.Tenant, r.PathValue("run"))
	if err != nil {
		httpErr(w, 500, err.Error())
		return
	}
	if out == nil {
		out = []runlog.Frame{}
	}
	writeJSON(w, out)
}
