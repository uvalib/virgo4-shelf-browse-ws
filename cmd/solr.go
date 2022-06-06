package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type solrRequestParams struct {
	DefType string   `json:"defType,omitempty"`
	Qt      string   `json:"qt,omitempty"`
	Sort    string   `json:"sort,omitempty"`
	Start   int      `json:"start"`
	Rows    int      `json:"rows"`
	Fl      []string `json:"fl,omitempty"`
	Fq      []string `json:"fq,omitempty"`
	Q       string   `json:"q,omitempty"`
}

type solrRequestJSON struct {
	Params solrRequestParams `json:"params"`
}

type solrMeta struct {
	maxScore  float32
	start     int
	numRows   int // for client pagination -- numGroups or numRecords
	totalRows int // for client pagination -- totalGroups or totalRecords
}

type solrRequest struct {
	json solrRequestJSON
	meta solrMeta
}

type solrResponseHeader struct {
	Status int `json:"status,omitempty"`
	QTime  int `json:"QTime,omitempty"`
}

type solrDocument map[string]interface{}

type solrResponseDocuments struct {
	NumFound int            `json:"numFound,omitempty"`
	Start    int            `json:"start,omitempty"`
	MaxScore float32        `json:"maxScore,omitempty"`
	Docs     []solrDocument `json:"docs,omitempty"`
}

type solrError struct {
	Metadata []string `json:"metadata,omitempty"`
	Msg      string   `json:"msg,omitempty"`
	Code     int      `json:"code,omitempty"`
}

type solrResponse struct {
	ResponseHeader solrResponseHeader       `json:"responseHeader,omitempty"`
	Response       solrResponseDocuments    `json:"response,omitempty"`
	Debug          interface{}              `json:"debug,omitempty"`
	Terms          map[string][]interface{} `json:"terms,omitempty"`
	Error          solrError                `json:"error,omitempty"`
	Status         string                   `json:"status,omitempty"`
	meta           *solrMeta                // pointer to struct in corresponding solrRequest
}

func (s *solrDocument) getRawValue(field string) interface{} {
	return (*s)[field]
}

func (s *solrDocument) getStrings(field string) []string {
	// turn all potential values into string slices

	v := s.getRawValue(field)

	switch t := v.(type) {
	case []interface{}:
		vals := make([]string, len(t))
		for i, val := range t {
			vals[i] = val.(string)
		}
		return vals

	case []string:
		return t

	case string:
		return []string{t}

	case float32:
		return []string{fmt.Sprintf("%0.8f", t)}

	default:
		return []string{}
	}
}

func (s *solrDocument) getFirstString(field string) string {
	// shortcut to get first value for multi-value fields that really only ever contain one value
	return firstElementOf(s.getStrings(field))
}

func (s *searchContext) buildSolrItemRequest(query string) {
	var req solrRequest

	//	req.meta.client = s.virgoReq.meta.client

	req.json.Params.Q = query
	req.json.Params.Qt = s.svc.config.Solr.Params.Qt
	req.json.Params.DefType = s.svc.config.Solr.Params.DefType
	req.json.Params.Fq = nonemptyValues(s.svc.config.Solr.Params.Fq)
	req.json.Params.Fl = nonemptyValues(s.svc.config.Solr.Params.Fl)
	req.json.Params.Start = 0
	req.json.Params.Rows = 1

	s.solrReq = &req
}

func (s *searchContext) solrItemQuery(query string) error {
	ctx := s.svc.solr.service

	s.buildSolrItemRequest(query)

	jsonBytes, jsonErr := json.Marshal(s.solrReq.json)
	if jsonErr != nil {
		s.log("[SOLR] Marshal() failed: %s", jsonErr.Error())
		return fmt.Errorf("failed to marshal Solr JSON")
	}

	// we cannot use query parameters for the request due to the
	// possibility of triggering a 414 response (URI Too Long).

	// instead, write the json to the body of the request.
	// NOTE: Solr is lenient; GET or POST works fine for this.

	req, reqErr := http.NewRequest("POST", ctx.url, bytes.NewBuffer(jsonBytes))
	if reqErr != nil {
		s.log("[SOLR] NewRequest() failed: %s", reqErr.Error())
		return fmt.Errorf("failed to create Solr request")
	}

	req.Header.Set("Content-Type", "application/json")

	if s.client.opts.verbose == true {
		s.log("[SOLR] req: [%s]", string(jsonBytes))
	} else {
		s.log("[SOLR] req: [%s]", s.solrReq.json.Params.Q)
	}

	start := time.Now()
	res, resErr := ctx.client.Do(req)
	elapsedMS := int64(time.Since(start) / time.Millisecond)

	// external service failure logging (scenario 1)

	if resErr != nil {
		status := http.StatusBadRequest
		errMsg := resErr.Error()
		if strings.Contains(errMsg, "Timeout") {
			status = http.StatusRequestTimeout
			errMsg = fmt.Sprintf("%s timed out", ctx.url)
		} else if strings.Contains(errMsg, "connection refused") {
			status = http.StatusServiceUnavailable
			errMsg = fmt.Sprintf("%s refused connection", ctx.url)
		}

		s.log("[SOLR] client.Do() failed: %s", resErr.Error())
		s.log("ERROR: Failed response from %s %s - %d:%s. Elapsed Time: %d (ms)", req.Method, ctx.url, status, errMsg, elapsedMS)
		return fmt.Errorf("failed to receive Solr response")
	}

	defer res.Body.Close()

	var solrRes solrResponse

	decoder := json.NewDecoder(res.Body)

	// external service failure logging (scenario 2)

	if decErr := decoder.Decode(&solrRes); decErr != nil {
		s.log("[SOLR] Decode() failed: %s", decErr.Error())
		s.log("ERROR: Failed response from %s %s - %d:%s. Elapsed Time: %d (ms)", req.Method, ctx.url, http.StatusInternalServerError, decErr.Error(), elapsedMS)
		return fmt.Errorf("failed to decode Solr response")
	}

	// external service success logging

	s.log("Successful Solr response from %s %s. Elapsed Time: %d (ms)", req.Method, ctx.url, elapsedMS)

	s.solrRes = &solrRes

	// log abbreviated results

	logHeader := fmt.Sprintf("[SOLR] res: header: { status = %d, QTime = %d }", solrRes.ResponseHeader.Status, solrRes.ResponseHeader.QTime)

	// quick validation
	if solrRes.ResponseHeader.Status != 0 {
		s.log("%s, error: { code = %d, msg = %s }", logHeader, solrRes.Error.Code, solrRes.Error.Msg)
		return fmt.Errorf("%d - %s", solrRes.Error.Code, solrRes.Error.Msg)
	}

	s.solrRes.meta = &s.solrReq.meta
	s.solrRes.meta.start = s.solrReq.json.Params.Start
	s.solrRes.meta.numRows = len(s.solrRes.Response.Docs)
	s.solrRes.meta.totalRows = s.solrRes.Response.NumFound

	s.log("%s, body: { start = %d, rows = %d, total = %d, maxScore = %0.2f }", logHeader, solrRes.meta.start, solrRes.meta.numRows, solrRes.meta.totalRows, solrRes.meta.maxScore)

	return nil
}

func (s *searchContext) solrPing() error {
	ctx := s.svc.solr.healthCheck

	req, reqErr := http.NewRequest("GET", ctx.url, nil)
	if reqErr != nil {
		s.log("[SOLR] NewRequest() failed: %s", reqErr.Error())
		return fmt.Errorf("failed to create Solr request")
	}

	start := time.Now()
	res, resErr := ctx.client.Do(req)
	elapsedMS := int64(time.Since(start) / time.Millisecond)

	// external service failure logging (scenario 1)

	if resErr != nil {
		status := http.StatusBadRequest
		errMsg := resErr.Error()
		if strings.Contains(errMsg, "Timeout") {
			status = http.StatusRequestTimeout
			errMsg = fmt.Sprintf("%s timed out", ctx.url)
		} else if strings.Contains(errMsg, "connection refused") {
			status = http.StatusServiceUnavailable
			errMsg = fmt.Sprintf("%s refused connection", ctx.url)
		}

		s.log("[SOLR] client.Do() failed: %s", resErr.Error())
		s.log("ERROR: Failed response from %s %s - %d:%s. Elapsed Time: %d (ms)", req.Method, ctx.url, status, errMsg, elapsedMS)
		return fmt.Errorf("failed to receive Solr response")
	}

	defer res.Body.Close()

	var solrRes solrResponse

	decoder := json.NewDecoder(res.Body)

	// external service failure logging (scenario 2)

	if decErr := decoder.Decode(&solrRes); decErr != nil {
		s.log("[SOLR] Decode() failed: %s", decErr.Error())
		s.log("ERROR: Failed response from %s %s - %d:%s. Elapsed Time: %d (ms)", req.Method, ctx.url, http.StatusInternalServerError, decErr.Error(), elapsedMS)
		return fmt.Errorf("failed to decode Solr response")
	}

	// external service success logging

	s.log("Successful Solr response from %s %s. Elapsed Time: %d (ms)", req.Method, ctx.url, elapsedMS)

	logHeader := fmt.Sprintf("[SOLR] res: header: { status = %d, QTime = %d }", solrRes.ResponseHeader.Status, solrRes.ResponseHeader.QTime)

	// quick validation
	if solrRes.ResponseHeader.Status != 0 {
		s.log("%s, error: { code = %d, msg = %s }", logHeader, solrRes.Error.Code, solrRes.Error.Msg)
		return fmt.Errorf("%d - %s", solrRes.Error.Code, solrRes.Error.Msg)
	}

	s.log("%s, ping status: %s", logHeader, solrRes.Status)

	if solrRes.Status != "OK" {
		return fmt.Errorf("ping status was not OK")
	}

	return nil
}

func (s *searchContext) solrTerms(field, key string, limit int) ([]string, error) {
	ctx := s.svc.solr.shelfBrowse

	req, reqErr := http.NewRequest("GET", ctx.url, nil)
	if reqErr != nil {
		s.log("SOLR: NewRequest() failed: %s", reqErr.Error())
		return nil, fmt.Errorf("failed to create Solr request")
	}

	// request a generous buffer of extra terms, in case it turns out some don't belong to any record.
	// this greatly increases the chance that the caller can fill the entire requested range.
	overage := 10 * limit

	qp := req.URL.Query()

	qp.Add("terms.fl", field)
	qp.Add("terms.lower", key)
	qp.Add("terms.lower.incl", "false")
	qp.Add("terms.limit", fmt.Sprintf("%d", overage))
	qp.Add("terms.sort", "index")

	req.URL.RawQuery = qp.Encode()

	if s.client.opts.verbose == true {
		s.log("SOLR: req: [%s]", req.URL.String())
	}

	start := time.Now()
	res, resErr := ctx.client.Do(req)
	elapsedMS := int64(time.Since(start) / time.Millisecond)

	// external service failure logging (scenario 1)

	if resErr != nil {
		status := http.StatusBadRequest
		errMsg := resErr.Error()
		if strings.Contains(errMsg, "Timeout") {
			status = http.StatusRequestTimeout
			errMsg = fmt.Sprintf("%s timed out", ctx.url)
		} else if strings.Contains(errMsg, "connection refused") {
			status = http.StatusServiceUnavailable
			errMsg = fmt.Sprintf("%s refused connection", ctx.url)
		}

		s.log("SOLR: client.Do() failed: %s", resErr.Error())
		s.log("ERROR: Failed response from %s %s - %d:%s. Elapsed Time: %d (ms)", req.Method, ctx.url, status, errMsg, elapsedMS)
		return nil, fmt.Errorf("failed to receive Solr response")
	}

	defer res.Body.Close()

	var solrRes solrResponse

	decoder := json.NewDecoder(res.Body)

	// external service failure logging (scenario 2)

	if decErr := decoder.Decode(&solrRes); decErr != nil {
		s.log("SOLR: Decode() failed: %s", decErr.Error())
		s.log("ERROR: Failed response from %s %s - %d:%s. Elapsed Time: %d (ms)", req.Method, ctx.url, http.StatusInternalServerError, decErr.Error(), elapsedMS)
		return nil, fmt.Errorf("failed to decode Solr response")
	}

	// external service success logging

	s.log("Successful Solr response from %s %s. Elapsed Time: %d (ms)", req.Method, ctx.url, elapsedMS)

	logHeader := fmt.Sprintf("SOLR: res: header: { status = %d, QTime = %d }", solrRes.ResponseHeader.Status, solrRes.ResponseHeader.QTime)

	// quick validation
	if solrRes.ResponseHeader.Status != 0 {
		s.log("%s, error: { code = %d, msg = %s }", logHeader, solrRes.Error.Code, solrRes.Error.Msg)
		return nil, fmt.Errorf("%d - %s", solrRes.Error.Code, solrRes.Error.Msg)
	}

	// build terms list

	var terms []string

	for i, term := range solrRes.Terms[field] {
		if i%2 == 0 {
			//s.log("[TERM] %s: [%s]", field, term)
			terms = append(terms, term.(string))
		}
	}

	return terms, nil
}
