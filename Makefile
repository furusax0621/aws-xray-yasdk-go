DEP_XRAYAWS=$(shell cat xrayaws/go.mod | grep -v module | grep github.com/shogo82148/aws-xray-yasdk-go | cut -d' ' -f2)

.PHONEY: test
test:
	go test -race -v -coverprofile=profile.cov ./...

	# Take care of Multi-Module Repositories
	# ref. https://github.com/golang/go/wiki/Modules#faqs--multi-module-repositories
	cd xrayaws && go mod edit -replace github.com/shogo82148/aws-xray-yasdk-go@${DEP_XRAYAWS}=../ && \
		go test -race -v -coverprofile=profile.cov ./... && \
		go mod edit -dropreplace github.com/shogo82148/aws-xray-yasdk-go@${DEP_XRAYAWS}
	cat xrayaws/profile.cov | grep -v '^mode:' >> profile.cov
