package apiclient

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ── Postman v2.1 types ──────────────────────────────────────────────────────

type pmCollection struct {
	Info pmInfo   `json:"info"`
	Item []pmItem `json:"item"`
}

type pmInfo struct {
	Name   string `json:"name"`
	Schema string `json:"schema"`
}

type pmItem struct {
	Name    string    `json:"name"`
	Item    []pmItem  `json:"item,omitempty"`
	Request *pmReq    `json:"request,omitempty"`
	Event   []pmEvent `json:"event,omitempty"`
}

type pmEvent struct {
	Listen string   `json:"listen"`
	Script pmScript `json:"script"`
}

type pmScript struct {
	Type string   `json:"type"`
	Exec []string `json:"exec"`
}

type pmReq struct {
	Method string  `json:"method"`
	Header []pmKV  `json:"header"`
	URL    pmURL   `json:"url"`
	Body   *pmBody `json:"body,omitempty"`
	Auth   *pmAuth `json:"auth,omitempty"`
}

type pmKV struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	Type     string `json:"type,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
}

type pmURL struct {
	Raw   string `json:"raw"`
	Query []pmKV `json:"query,omitempty"`
}

type pmBody struct {
	Mode       string  `json:"mode"`
	Raw        string  `json:"raw,omitempty"`
	URLEncoded []pmKV  `json:"urlencoded,omitempty"`
	FormData   []pmKV  `json:"formdata,omitempty"`
	Options    pmOpts  `json:"options,omitempty"`
}

type pmOpts struct {
	Raw pmRawOpt `json:"raw,omitempty"`
}

type pmRawOpt struct {
	Language string `json:"language,omitempty"`
}

type pmAuth struct {
	Type   string `json:"type"`
	Bearer []pmKV `json:"bearer,omitempty"`
	Basic  []pmKV `json:"basic,omitempty"`
	APIKey []pmKV `json:"apikey,omitempty"`
}

// ── Export ─────────────────────────────────────────────────────────────────

func (d *DB) ExportCollection(collID string) (string, error) {
	var name string
	err := d.db.QueryRow(`SELECT name FROM collections WHERE id=?`, collID).Scan(&name)
	if err != nil {
		return "", fmt.Errorf("collection not found: %w", err)
	}

	rows, err := d.db.Query(
		`SELECT id, name, method, url, params_json, headers_json, body_type, body_content, auth_type, auth_json
		 FROM requests WHERE collection_id=? ORDER BY sort_order, name`, collID)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var items []pmItem
	for rows.Next() {
		var r SavedRequest
		var pj, hj, aj string
		rows.Scan(&r.ID, &r.Name, &r.Method, &r.URL, &pj, &hj, &r.BodyType, &r.BodyContent, &r.AuthType, &aj)
		json.Unmarshal([]byte(pj), &r.Params)
		json.Unmarshal([]byte(hj), &r.Headers)
		json.Unmarshal([]byte(aj), &r.AuthData)
		items = append(items, reqToPmItem(r))
	}
	if items == nil {
		items = []pmItem{}
	}

	pc := pmCollection{
		Info: pmInfo{
			Name:   name,
			Schema: "https://schema.getpostman.com/json/collection/v2.1.0/collection.json",
		},
		Item: items,
	}
	b, err := json.MarshalIndent(pc, "", "  ")
	return string(b), err
}

func reqToPmItem(r SavedRequest) pmItem {
	req := &pmReq{
		Method: r.Method,
		URL:    pmURL{Raw: r.URL},
	}

	for _, h := range r.Headers {
		if h.Key != "" {
			req.Header = append(req.Header, pmKV{Key: h.Key, Value: h.Value, Type: "text", Disabled: !h.Enabled})
		}
	}

	for _, p := range r.Params {
		if p.Key != "" {
			req.URL.Query = append(req.URL.Query, pmKV{Key: p.Key, Value: p.Value, Disabled: !p.Enabled})
		}
	}

	if r.BodyType != "none" && (r.BodyContent != "" || r.BodyType != "raw") {
		req.Body = &pmBody{}
		switch r.BodyType {
		case "raw":
			req.Body.Mode = "raw"
			req.Body.Raw = r.BodyContent
			req.Body.Options = pmOpts{Raw: pmRawOpt{Language: "json"}}
		case "urlencoded":
			req.Body.Mode = "urlencoded"
		case "form-data":
			req.Body.Mode = "formdata"
		}
	}

	if r.AuthType != "none" && r.AuthData != nil {
		req.Auth = &pmAuth{Type: r.AuthType}
		switch r.AuthType {
		case "bearer":
			req.Auth.Bearer = []pmKV{{Key: "token", Value: r.AuthData["token"]}}
		case "basic":
			req.Auth.Basic = []pmKV{
				{Key: "username", Value: r.AuthData["username"]},
				{Key: "password", Value: r.AuthData["password"]},
			}
		case "apikey-header", "apikey-query":
			req.Auth.APIKey = []pmKV{
				{Key: "key", Value: r.AuthData["key"]},
				{Key: "value", Value: r.AuthData["value"]},
			}
		}
	}

	item := pmItem{Name: r.Name, Request: req}
	return item
}

// ── Import ─────────────────────────────────────────────────────────────────

func (d *DB) ImportCollection(wsID, jsonStr string) error {
	var pc pmCollection
	if err := json.Unmarshal([]byte(jsonStr), &pc); err != nil {
		return fmt.Errorf("parse JSON: %w", err)
	}
	if pc.Info.Name == "" {
		pc.Info.Name = "Imported Collection"
	}

	col, err := d.SaveCollection(Collection{
		WorkspaceID: wsID,
		Name:        pc.Info.Name,
	})
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}

	return d.importPmItems(wsID, col.ID, pc.Item)
}

func (d *DB) importPmItems(wsID, collID string, items []pmItem) error {
	for _, item := range items {
		if item.Request == nil {
			// folder — create sub-collection, recurse
			sub, err := d.SaveCollection(Collection{
				WorkspaceID: wsID,
				ParentID:    collID,
				Name:        item.Name,
			})
			if err != nil {
				continue
			}
			d.importPmItems(wsID, sub.ID, item.Item)
		} else {
			r := pmItemToReq(wsID, collID, item)
			d.SaveRequest(r)
		}
	}
	return nil
}

func pmItemToReq(wsID, collID string, item pmItem) SavedRequest {
	req := item.Request
	r := SavedRequest{
		WorkspaceID:  wsID,
		CollectionID: collID,
		Name:         item.Name,
		Method:       strings.ToUpper(req.Method),
		URL:          req.URL.Raw,
		AuthType:     "none",
		AuthData:     map[string]string{},
	}

	for _, h := range req.Header {
		if h.Key != "" {
			r.Headers = append(r.Headers, KVPair{Key: h.Key, Value: h.Value, Enabled: !h.Disabled})
		}
	}

	for _, p := range req.URL.Query {
		if p.Key != "" {
			r.Params = append(r.Params, KVPair{Key: p.Key, Value: p.Value, Enabled: !p.Disabled})
		}
	}

	if req.Body != nil {
		switch req.Body.Mode {
		case "raw":
			r.BodyType = "raw"
			r.BodyContent = req.Body.Raw
		case "urlencoded":
			r.BodyType = "urlencoded"
		case "formdata":
			r.BodyType = "form-data"
		}
	}

	if req.Auth != nil {
		switch req.Auth.Type {
		case "bearer":
			r.AuthType = "bearer"
			for _, kv := range req.Auth.Bearer {
				if kv.Key == "token" {
					r.AuthData["token"] = kv.Value
				}
			}
		case "basic":
			r.AuthType = "basic"
			for _, kv := range req.Auth.Basic {
				r.AuthData[kv.Key] = kv.Value
			}
		case "apikey":
			r.AuthType = "apikey-header"
			for _, kv := range req.Auth.APIKey {
				r.AuthData[kv.Key] = kv.Value
			}
		}
	}

	// extract pre-request script if present
	for _, ev := range item.Event {
		if ev.Listen == "prerequest" {
			r.BodyContent = strings.Join(ev.Script.Exec, "\n")
		}
	}

	if r.Params == nil {
		r.Params = []KVPair{}
	}
	if r.Headers == nil {
		r.Headers = []KVPair{}
	}
	return r
}
