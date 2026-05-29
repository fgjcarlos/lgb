package server

import "net/http"

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
// Returns the read-only view of the gateway's PLC tag mappings derived from
// the loaded config. Write operations are intentionally not exposed — the
// authoritative source is the YAML config and the watcher hot-reloads it.
func (s *Server) handleConfigMappings(w http.ResponseWriter, _ *http.Request) {
	if s.cfg == nil {
		writeJSON(w, http.StatusOK, struct {
			Data []mappingResponse `json:"data"`
		}{Data: []mappingResponse{}})
		return
	}

	rows := make([]mappingResponse, 0, len(s.cfg.PLCs))
	for _, plc := range s.cfg.PLCs {
		tags := make([]mappingTagResponse, 0, len(plc.Tags))
		for _, t := range plc.Tags {
			tags = append(tags, mappingTagResponse{Name: t.Name, Type: t.Type})
		}
		rows = append(rows, mappingResponse{
			PLC:      plc.Name,
			Address:  plc.Address,
			ScanRate: plc.ScanRate,
			Tags:     tags,
		})
	}

	writeJSON(w, http.StatusOK, struct {
		Data []mappingResponse `json:"data"`
	}{Data: rows})
}
