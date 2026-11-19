.PHONY: run build clean

# There's no Vite here. templates/ are written by hand and the look comes from
# suite/. The database goes in data/, which clean leaves alone.
run:
	MCP_DB=data/mcp.db go run .

build:
	go build -tags embed -trimpath -ldflags="-s -w" -o ../../bin/mcp.bythewood.me .

clean:
	rm -f ../../bin/mcp.bythewood.me
