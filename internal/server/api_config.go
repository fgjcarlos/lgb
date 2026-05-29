package server

import (
	"net/http"

	"github.com/fgjcarlos/lgb/internal/config"
)

type mappingTagResponse struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type mappingResponse struct {
	PLC      string               `json:"plc"`
	Address  string               `json:"address"`
	ScanRate string               `json:"scan_rate"`
	Tags     []mappingTagResponse `json:"tags"`
}

// handleConfigMappings serves GET /api/config/mappings.
// When a PLCStore is wired it queries the store directly on every request,
// guaranteeing the response reflects post-mutation state (PCS-API-2.6 read-path
// redirect). The frozen s.cfg is used only as a fallback for no-store deployments.
func (s *Server) handleConfigMappings(w http.ResponseWriter, r *http.Request) {
	var plcs []config.PLC

	if s.plcStore != nil {
		var err error
		plcs, err = s.plcStore.List(r.Context())
		if err != nil {
			s.log.Warn("plc store list failed in mappings handler", "err", err)
			plcs = nil
		}
	} else if s.cfg != nil {
		plcs = s.cfg.PLCs
	}

	rows := make([]mappingResponse, 0, len(plcs))
	for _, p := range plcs {
		tags := make([]mappingTagResponse, 0, len(p.Tags))
		for _, t := range p.Tags {
			tags = append(tags, mappingTagResponse{Name: t.Name, Type: t.Type})
		}
		rows = append(rows, mappingResponse{
			PLC:      p.Name,
			Address:  p.Address,
			ScanRate: p.ScanRate,
			Tags:     tags,
		})
	}

	writeJSON(w, http.StatusOK, struct {
		Data []mappingResponse `json:"data"`
	}{Data: rows})
}
