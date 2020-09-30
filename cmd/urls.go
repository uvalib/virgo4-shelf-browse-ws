package main

import (
	"net/http"
	"strings"
)

func (s *searchContext) getCoverImageURL(doc *solrDocument) string {
	// compose a (minimal) url to the cover image service

	cfg := s.svc.config.Solr.CoverImages

	id := doc.getFirstString(cfg.IDField)

	url := cfg.URLPrefix + id

	// also add query parameters:
	// doc_type: music or non_music
	// books require at least one of: isbn, oclc, lccn, upc
	// music requires: artist_name, album_name
	// all else is optional

	// build query parameters using http package to properly quote values
	req, reqErr := http.NewRequest("GET", url, nil)
	if reqErr != nil {
		return ""
	}

	qp := req.URL.Query()

	title := doc.getFirstString(cfg.TitleField)
	poolValues := doc.getStrings(cfg.PoolField)

	// get author from first field with a value
	authorValue := ""
	for _, field := range cfg.AuthorFields {
		if authorValue = doc.getFirstString(field); authorValue != "" {
			s.log("field [%s] had author %s", field, authorValue)
			break
		}
	}

	// remove extraneous dates from author
	author := strings.Trim(strings.Split(authorValue, "[")[0], " ")

	s.log("author = [%s]", author)

	if sliceContainsString(poolValues, cfg.MusicPool) == true {
		// music

		qp.Add("doc_type", "music")

		if len(author) > 0 {
			qp.Add("artist_name", author)
		}

		if len(title) > 0 {
			qp.Add("album_name", title)
		}
	} else {
		// books... and everything else

		qp.Add("doc_type", "non_music")

		if len(title) > 0 {
			qp.Add("title", title)
		}
	}

	// always throw these optional values at the cover image service

	isbnValues := doc.getStrings(cfg.ISBNField)
	if len(isbnValues) > 0 {
		qp.Add("isbn", strings.Join(isbnValues, ","))
	}

	oclcValues := doc.getStrings(cfg.OCLCField)
	if len(oclcValues) > 0 {
		qp.Add("oclc", strings.Join(oclcValues, ","))
	}

	lccnValues := doc.getStrings(cfg.LCCNField)
	if len(lccnValues) > 0 {
		qp.Add("lccn", strings.Join(lccnValues, ","))
	}

	upcValues := doc.getStrings(cfg.UPCField)
	if len(upcValues) > 0 {
		qp.Add("upc", strings.Join(upcValues, ","))
	}

	req.URL.RawQuery = qp.Encode()

	return req.URL.String()
}
