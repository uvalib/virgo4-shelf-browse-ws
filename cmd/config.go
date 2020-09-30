package main

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"sort"
	"strings"
)

const envPrefix = "VIRGO4_SHELF_BROWSE_WS"

type serviceConfigSolrParams struct {
	Qt      string   `json:"qt,omitempty"`
	DefType string   `json:"deftype,omitempty"`
	Fq      []string `json:"fq,omitempty"`
	Fl      []string `json:"fl,omitempty"`
}

type serviceConfigSolrClient struct {
	Endpoint    string `json:"endpoint,omitempty"`
	ConnTimeout string `json:"conn_timeout,omitempty"`
	ReadTimeout string `json:"read_timeout,omitempty"`
}

type serviceConfigSolrClients struct {
	Service     serviceConfigSolrClient `json:"service,omitempty"`
	HealthCheck serviceConfigSolrClient `json:"healthcheck,omitempty"`
	ShelfBrowse serviceConfigSolrClient `json:"shelf_browse,omitempty"`
}

type serviceConfigSolrShelfBrowse struct {
	ForwardKey   string `json:"forward_key,omitempty"`
	ReverseKey   string `json:"reverse_key,omitempty"`
	DefaultItems int    `json:"default_items,omitempty"`
	MaxItems     int    `json:"max_items,omitempty"`
}

type serviceConfigCoverImages struct {
	URLPrefix    string   `json:"url_prefix,omitempty"`
	IDField      string   `json:"id_field,omitempty"`
	TitleField   string   `json:"title_field,omitempty"`
	AuthorFields []string `json:"author_fields,omitempty"`
	ISBNField    string   `json:"isbn_field,omitempty"`
	LCCNField    string   `json:"lccn_field,omitempty"`
	OCLCField    string   `json:"oclc_field,omitempty"`
	PoolField    string   `json:"pool_field,omitempty"`
	UPCField     string   `json:"upc_field,omitempty"`
	MusicPool    string   `json:"music_pool,omitempty"`
}

type serviceConfigSolr struct {
	Host        string                       `json:"host,omitempty"`
	Core        string                       `json:"core,omitempty"`
	Clients     serviceConfigSolrClients     `json:"clients,omitempty"`
	Params      serviceConfigSolrParams      `json:"params,omitempty"`
	ShelfBrowse serviceConfigSolrShelfBrowse `json:"shelf_browse,omitempty"`
	CoverImages serviceConfigCoverImages     `json:"cover_images,omitempty"`
}

type serviceConfigField struct {
	Name  string `json:"name,omitempty"`
	Field string `json:"field,omitempty"`
}

type serviceConfig struct {
	Port   string               `json:"port,omitempty"`
	JWTKey string               `json:"jwt_key,omitempty"`
	Solr   serviceConfigSolr    `json:"solr,omitempty"`
	Fields []serviceConfigField `json:"fields,omitempty"`
}

func getSortedJSONEnvVars() []string {
	var keys []string

	for _, keyval := range os.Environ() {
		key := strings.Split(keyval, "=")[0]
		if strings.HasPrefix(key, envPrefix+"_JSON_") {
			keys = append(keys, key)
		}
	}

	sort.Strings(keys)

	return keys
}

func loadConfig() *serviceConfig {
	cfg := serviceConfig{}

	// json configs

	envs := getSortedJSONEnvVars()

	valid := true

	for _, env := range envs {
		log.Printf("[CONFIG] loading %s ...", env)
		if val := os.Getenv(env); val != "" {
			dec := json.NewDecoder(bytes.NewReader([]byte(val)))
			dec.DisallowUnknownFields()

			if err := dec.Decode(&cfg); err != nil {
				log.Printf("error decoding %s: %s", env, err.Error())
				valid = false
			}
		}
	}

	if valid == false {
		log.Printf("exiting due to json decode error(s) above")
		os.Exit(1)
	}

	// optional convenience override to simplify terraform config
	if host := os.Getenv(envPrefix + "_SOLR_HOST"); host != "" {
		cfg.Solr.Host = host
	}

	bytes, err := json.Marshal(cfg)
	if err != nil {
		log.Printf("error encoding config json: %s", err.Error())
		os.Exit(1)
	}

	log.Printf("[CONFIG] composite json:\n%s", string(bytes))

	return &cfg
}
