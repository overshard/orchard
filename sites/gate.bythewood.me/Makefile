.PHONY: run build clean

# There's no Vite here. templates/ are written by hand and the look comes from
# suite/. The database goes in data/, which clean leaves alone.
run:
	GATE_DATA=data go run .

build:
	go build -tags embed -trimpath -ldflags="-s -w" -o ../../bin/gate.bythewood.me .

clean:
	rm -f ../../bin/gate.bythewood.me
