.PHONY: run build clean

# There's no Vite here. templates/ are written by hand and the look comes from
# suite/. The database and lists go in data/, which clean leaves alone. Point
# DNS_UPSTREAM at any resolver for a dev run, like 1.1.1.1:53.
run:
	DNS_DATA=data DNS_TLS=data/tls DNS_UPSTREAM=$${DNS_UPSTREAM:-1.1.1.1:53} go run .

build:
	go build -tags embed -trimpath -ldflags="-s -w" -o ../../bin/dns.bythewood.me .

clean:
	rm -f ../../bin/dns.bythewood.me
