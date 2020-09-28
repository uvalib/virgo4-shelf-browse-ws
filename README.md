# Virgo4 Shelf Browse Web Service

This is a web service to facilitate virtual shelf browsing using data from Solr.

* GET /version : returns build version
* GET /healthcheck : returns health check information
* GET /metrics : returns Prometheus metrics
* GET /api/browse/{id}?range=N : returns shelf browse information for up to N records surrounding the item with id {id}

All endpoints under /api require authentication.

### System Requirements

* GO version 1.12.0 or greater
