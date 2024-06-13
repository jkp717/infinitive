default: infinitive

bindata_assetfs.go: assets/ui.html assets/app/app.js assets/index.html
	go-bindata-assetfs assets/... && mv bindata.go bindata_assetfs.go

infinitive: bindata_assetfs.go cache.go conversions.go dispatcher.go frame.go infinitive.go protocol.go tables.go webserver.go
	go build infinitive
