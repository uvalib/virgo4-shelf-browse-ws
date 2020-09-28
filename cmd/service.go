package main

import (
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// git commit used for this build; supplied at compile time
var gitCommit string

type serviceVersion struct {
	BuildVersion string `json:"build,omitempty"`
	GoVersion    string `json:"go_version,omitempty"`
	GitCommit    string `json:"git_commit,omitempty"`
}

type serviceSolrContext struct {
	client *http.Client
	url    string
}

type serviceSolr struct {
	service     serviceSolrContext
	healthCheck serviceSolrContext
	shelfBrowse serviceSolrContext
}

type serviceContext struct {
	randomSource *rand.Rand
	config       *serviceConfig
	version      serviceVersion
	solr         serviceSolr
}

type stringValidator struct {
	values  []string
	invalid bool
}

func (v *stringValidator) addValue(value string) {
	if value != "" {
		v.values = append(v.values, value)
	}
}

func (v *stringValidator) requireValue(value string, label string) {
	if value == "" {
		log.Printf("[VALIDATE] missing %s", label)
		v.invalid = true
		return
	}

	v.addValue(value)
}

func (v *stringValidator) Values() []string {
	return v.values
}

func (v *stringValidator) Invalid() bool {
	return v.invalid
}

func (p *serviceContext) initVersion() {
	buildVersion := "unknown"
	files, _ := filepath.Glob("buildtag.*")
	if len(files) == 1 {
		buildVersion = strings.Replace(files[0], "buildtag.", "", 1)
	}

	p.version = serviceVersion{
		BuildVersion: buildVersion,
		GoVersion:    fmt.Sprintf("%s %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH),
		GitCommit:    gitCommit,
	}

	log.Printf("[SERVICE] version.BuildVersion = [%s]", p.version.BuildVersion)
	log.Printf("[SERVICE] version.GoVersion    = [%s]", p.version.GoVersion)
	log.Printf("[SERVICE] version.GitCommit    = [%s]", p.version.GitCommit)
}

func httpClientWithTimeouts(conn, read string) *http.Client {
	connTimeout := integerWithMinimum(conn, 1)
	readTimeout := integerWithMinimum(read, 1)

	client := &http.Client{
		Timeout: time.Duration(readTimeout) * time.Second,
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   time.Duration(connTimeout) * time.Second,
				KeepAlive: 60 * time.Second,
			}).DialContext,
			MaxIdleConns:        100, // we are hitting one solr host, so
			MaxIdleConnsPerHost: 100, // these two values can be the same
			IdleConnTimeout:     90 * time.Second,
		},
	}

	return client
}

func (p *serviceContext) initSolr() {
	// client setup

	serviceCtx := serviceSolrContext{
		url:    fmt.Sprintf("%s/%s/%s", p.config.Solr.Host, p.config.Solr.Core, p.config.Solr.Clients.Service.Endpoint),
		client: httpClientWithTimeouts(p.config.Solr.Clients.Service.ConnTimeout, p.config.Solr.Clients.Service.ReadTimeout),
	}

	healthCtx := serviceSolrContext{
		url:    fmt.Sprintf("%s/%s/%s", p.config.Solr.Host, p.config.Solr.Core, p.config.Solr.Clients.HealthCheck.Endpoint),
		client: httpClientWithTimeouts(p.config.Solr.Clients.HealthCheck.ConnTimeout, p.config.Solr.Clients.HealthCheck.ReadTimeout),
	}

	shelfBrowseCtx := serviceSolrContext{
		url:    fmt.Sprintf("%s/%s/%s", p.config.Solr.Host, p.config.Solr.Core, p.config.Solr.Clients.ShelfBrowse.Endpoint),
		client: httpClientWithTimeouts(p.config.Solr.Clients.ShelfBrowse.ConnTimeout, p.config.Solr.Clients.ShelfBrowse.ReadTimeout),
	}

	solr := serviceSolr{
		service:     serviceCtx,
		healthCheck: healthCtx,
		shelfBrowse: shelfBrowseCtx,
	}

	p.solr = solr

	log.Printf("[SERVICE] solr service url     = [%s]", serviceCtx.url)
	log.Printf("[SERVICE] solr healthCheck url = [%s]", healthCtx.url)
	log.Printf("[SERVICE] solr shelfBrowse url = [%s]", shelfBrowseCtx.url)
}

func (p *serviceContext) validateConfig() {
	// ensure the existence and validity of required variables/solr fields

	invalid := false

	var miscValues stringValidator

	miscValues.requireValue(p.config.Solr.Host, "solr host")
	miscValues.requireValue(p.config.Solr.Core, "solr core")
	miscValues.requireValue(p.config.Solr.Clients.Service.Endpoint, "solr service endpoint")
	miscValues.requireValue(p.config.Solr.Clients.HealthCheck.Endpoint, "solr healthcheck endpoint")
	miscValues.requireValue(p.config.Solr.Params.Qt, "solr param qt")
	miscValues.requireValue(p.config.Solr.Params.DefType, "solr param deftype")

	for _, field := range p.config.Fields {
		miscValues.requireValue(field.Name, "output field json name")
		miscValues.requireValue(field.Field, "output field solr field")
	}

	// check if anything went wrong anywhere

	if invalid || miscValues.Invalid() {
		log.Printf("[VALIDATE] exiting due to error(s) above")
		os.Exit(1)
	}
}

func initializeService(cfg *serviceConfig) *serviceContext {
	p := serviceContext{}

	p.config = cfg
	p.randomSource = rand.New(rand.NewSource(time.Now().UnixNano()))

	p.initVersion()
	p.initSolr()

	p.validateConfig()

	return &p
}
