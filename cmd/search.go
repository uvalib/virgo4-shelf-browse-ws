package main

import (
	"fmt"
	"net/http"
	"strconv"
)

type searchContext struct {
	svc     *serviceContext
	client  *clientContext
	solrReq *solrRequest
	solrRes *solrResponse
}

type searchResponse struct {
	status int         // http status code
	data   interface{} // data to return as JSON
	err    error       // error, if any
}

type shelfBrowseItem struct {
	doc        *solrDocument
	forwardKey string
	reverseKey string
}

type shelfBrowseResponse struct {
	Items         []map[string]string `json:"items,omitempty"`
	StatusCode    int                 `json:"status_code"`
	StatusMessage string              `json:"status_msg,omitempty"`
}

func (s *searchContext) init(p *serviceContext, c *clientContext) {
	s.svc = p
	s.client = c
}

func (s *searchContext) log(format string, args ...interface{}) {
	s.client.log(format, args...)
}

func (s *searchContext) err(format string, args ...interface{}) {
	s.client.err(format, args...)
}

func (s *searchContext) warn(format string, args ...interface{}) {
	s.client.warn(format, args...)
}

func (s *searchContext) performItemQuery(id string) searchResponse {
	if err := s.solrItemQuery(id); err != nil {
		s.err("query execution error: %s", err.Error())
		return searchResponse{status: http.StatusInternalServerError, err: err}
	}

	if s.solrRes.meta.numRows == 0 {
		err := fmt.Errorf("record not found")
		s.warn(err.Error())
		return searchResponse{status: http.StatusNotFound, err: err}
	}

	return searchResponse{status: http.StatusOK}
}

func (s *searchContext) getItemDetails(field, value string) (shelfBrowseItem, searchResponse) {
	var item shelfBrowseItem

	query := fmt.Sprintf(`%s:"%s"`, field, value)

	if resp := s.performItemQuery(query); resp.err != nil {
		return item, resp
	}

	doc := s.solrRes.Response.Docs[0]

	item.doc = &doc
	item.forwardKey = doc.getFirstString(s.svc.config.Solr.ShelfBrowse.ForwardKey)
	item.reverseKey = doc.getFirstString(s.svc.config.Solr.ShelfBrowse.ReverseKey)

	if item.forwardKey == "" && item.reverseKey == "" {
		err := fmt.Errorf("item does not have shelf keys")
		s.warn(err.Error())
		return item, searchResponse{status: http.StatusNotFound, err: err}
	}

	return item, searchResponse{status: http.StatusOK}
}

func (s *searchContext) handleBrowseRequest() searchResponse {
	id := s.client.ginCtx.Param("id")

	// get requested range
	limit := s.svc.config.Solr.ShelfBrowse.DefaultItems
	rng := s.client.ginCtx.Query("range")
	if r, err := strconv.Atoi(rng); rng != "" && err == nil {
		limit = r
	}

	// ensure requested range is reasonable
	switch {
	case limit <= 0:
		limit = s.svc.config.Solr.ShelfBrowse.DefaultItems
	case limit > s.svc.config.Solr.ShelfBrowse.MaxItems:
		limit = s.svc.config.Solr.ShelfBrowse.MaxItems
	}

	s.log("id = [%s]  range = [%s]  limit = [%d]", id, rng, limit)

	thisItem, thisResp := s.getItemDetails("id", id)

	if thisResp.err != nil {
		resp := thisResp
		resp.data = shelfBrowseResponse{StatusCode: resp.status, StatusMessage: resp.err.Error()}
		return resp
	}

	// get forward/reverse shelf keys for this item via solr terms query

	fwdKeys, fwdErr := s.solrTerms(s.svc.config.Solr.ShelfBrowse.ForwardKey, thisItem.forwardKey, limit)
	if fwdErr != nil {
		resp := searchResponse{status: http.StatusInternalServerError, err: fwdErr}
		resp.data = shelfBrowseResponse{StatusCode: resp.status, StatusMessage: resp.err.Error()}
		return resp
	}

	revKeys, revErr := s.solrTerms(s.svc.config.Solr.ShelfBrowse.ReverseKey, thisItem.reverseKey, limit)
	if revErr != nil {
		resp := searchResponse{status: http.StatusInternalServerError, err: revErr}
		resp.data = shelfBrowseResponse{StatusCode: resp.status, StatusMessage: resp.err.Error()}
		return resp
	}

	// build sequential list of items

	var items []shelfBrowseItem

	count := 0
	for _, key := range revKeys {
		//s.log("reverse key: [%s]", key)
		if revItem, revResp := s.getItemDetails(s.svc.config.Solr.ShelfBrowse.ReverseKey, key); revResp.err == nil {
			items = append([]shelfBrowseItem{revItem}, items...)
			count++
			if count >= limit {
				break
			}
		}
	}

	items = append(items, thisItem)

	count = 0
	for _, key := range fwdKeys {
		//s.log("forward key: [%s]", key)
		if fwdItem, fwdResp := s.getItemDetails(s.svc.config.Solr.ShelfBrowse.ForwardKey, key); fwdResp.err == nil {
			items = append(items, fwdItem)
			count++
			if count >= limit {
				break
			}
		}
	}

	// populate each item

	var itemMap []map[string]string

	for _, item := range items {
		newItem := make(map[string]string)

		for _, field := range s.svc.config.Fields {
			val := item.doc.getFirstString(field.Field)

			if val == "" && field.Name == "cover_image_url" {
				val = s.getCoverImageURL(item.doc)
			}

			if val != "" {
				newItem[field.Name] = val
			}
		}

		itemMap = append(itemMap, newItem)
	}

	// build response

	res := shelfBrowseResponse{Items: itemMap, StatusCode: http.StatusOK}

	return searchResponse{status: http.StatusOK, data: res}
}

func (s *searchContext) handlePingRequest() searchResponse {
	if err := s.solrPing(); err != nil {
		s.err("query execution error: %s", err.Error())
		return searchResponse{status: http.StatusInternalServerError, err: err}
	}

	return searchResponse{status: http.StatusOK}
}
