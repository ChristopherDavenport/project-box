package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

// The origins surface: an audit that starts from an issue or a PR has a door
// into the room. GET /v1/origins/{key} is the door itself — every
// origination whose delivery key equals key or extends it with a '-suffix'
// (a resubmission), newest first, each with its whole story. /issues/{n} and
// /prs/{n} are the two audiences that ask for it by a name other than the
// raw delivery key; /export hands the same story back as one downloadable
// artifact, the way /v1/provenance/{sha}/export does for a chain.

func (s *Server) origins(w http.ResponseWriter, r *http.Request) {
	out, err := s.room(r).Origins(r.Context(), r.PathValue("key"))
	if err != nil {
		httpErr(w, 500, err.Error())
		return
	}
	writeJSON(w, out)
}

func (s *Server) originsByIssue(w http.ResponseWriter, r *http.Request) {
	out, err := s.room(r).Origins(r.Context(), "issue-"+r.PathValue("n"))
	if err != nil {
		httpErr(w, 500, err.Error())
		return
	}
	writeJSON(w, out)
}

func (s *Server) originsByPR(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil {
		httpErr(w, http.StatusBadRequest, "pr number must be numeric: "+r.PathValue("n"))
		return
	}
	out, err := s.room(r).OriginsByPR(r.Context(), n)
	if err != nil {
		httpErr(w, 500, err.Error())
		return
	}
	writeJSON(w, out)
}

func (s *Server) originsExport(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	out, err := s.room(r).Origins(r.Context(), key)
	if err != nil {
		httpErr(w, 500, err.Error())
		return
	}
	body, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		httpErr(w, 500, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", "origins-"+key+".json"))
	w.Write(body)
}
