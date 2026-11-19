.PHONY: run build clean

# There's no Vite here. templates/ and static/ are written by hand and a dev
# build reads both off disk on every request. The database goes in data/,
# which clean leaves alone.
run:
	go run .

build:
	go build -tags embed -trimpath -ldflags="-s -w" -o ../../bin/cron.bythewood.me .

clean:
	rm -f ../../bin/cron.bythewood.me
